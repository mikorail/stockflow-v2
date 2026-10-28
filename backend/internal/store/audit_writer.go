package store

import (
	"context"
	"encoding/json"
	"log"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	auditQueueSize = 1024
	auditBatchSize = 100
	auditInterval  = 200 * time.Millisecond
)

type auditEvent struct {
	entityType string
	entityID   int
	label      string
	action     string
	details    []byte
	at         time.Time
}

func newAuditEvent(entityType string, id int, label, action string, details any) auditEvent {
	b, err := json.Marshal(details)
	if err != nil {
		b = []byte(`{}`)
	}
	return auditEvent{entityType: entityType, entityID: id, label: label, action: action, details: b, at: time.Now()}
}

type auditWriter struct {
	pool     *pgxpool.Pool
	ch       chan auditEvent
	flushReq chan chan struct{}
	quit     chan struct{}
	done     chan struct{}
	closed   atomic.Bool
}

func newAuditWriter(pool *pgxpool.Pool) *auditWriter {
	w := &auditWriter{
		pool:     pool,
		ch:       make(chan auditEvent, auditQueueSize),
		flushReq: make(chan chan struct{}),
		quit:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go w.run()
	return w
}

func (w *auditWriter) record(ev auditEvent) {
	if w.closed.Load() {
		w.insert([]auditEvent{ev})
		return
	}
	select {
	case w.ch <- ev:
	default:
		w.insert([]auditEvent{ev})
	}
}

func (w *auditWriter) run() {
	defer close(w.done)
	t := time.NewTicker(auditInterval)
	defer t.Stop()
	var batch []auditEvent
	flush := func() {
		if len(batch) > 0 {
			w.insert(batch)
			batch = batch[:0]
		}
	}
	drain := func() {
		for {
			select {
			case ev := <-w.ch:
				batch = append(batch, ev)
			default:
				return
			}
		}
	}
	for {
		select {
		case ev := <-w.ch:
			batch = append(batch, ev)
			if len(batch) >= auditBatchSize {
				flush()
			}
		case <-t.C:
			flush()
		case ack := <-w.flushReq:
			drain()
			flush()
			close(ack)
		case <-w.quit:
			drain()
			flush()
			return
		}
	}
}

func (w *auditWriter) insert(events []auditEvent) {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = w.tryInsert(ctx, events)
		cancel()
		if err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, e := range events {
		log.Printf("AUDIT WRITE FAILED (%v): %s %s %d %q %s at %s", err, e.action, e.entityType, e.entityID, e.label, e.details, e.at.Format(time.RFC3339Nano))
	}
}

func (w *auditWriter) tryInsert(ctx context.Context, events []auditEvent) error {
	b := &pgx.Batch{}
	for _, e := range events {
		b.Queue(`INSERT INTO audit_log (entity_type, entity_id, entity_label, action, details, created_at)
		  VALUES ($1,$2,$3,$4,$5::jsonb,$6)`, e.entityType, e.entityID, e.label, e.action, string(e.details), e.at)
	}
	res := w.pool.SendBatch(ctx, b)
	defer res.Close()
	for range events {
		if _, err := res.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func (w *auditWriter) flush(ctx context.Context) error {
	ack := make(chan struct{})
	select {
	case w.flushReq <- ack:
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-ack:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *auditWriter) close(ctx context.Context) error {
	if w.closed.Swap(true) {
		return nil
	}
	close(w.quit)
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
