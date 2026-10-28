INSERT INTO products (sku, name, price, stock, is_active) VALUES
 ('KB-001', 'Mechanical Keyboard TKL',       850000, 37, TRUE),
 ('MS-002', 'Wireless Mouse',                275000, 61, TRUE),
 ('MN-003', '27" IPS Monitor',              3250000, 12, TRUE),
 ('HD-004', 'USB-C Hub 7-in-1',              420000,  8, TRUE),
 ('WC-005', 'Webcam 1080p',                  560000, 25, TRUE),
 ('HS-006', 'Noise-Cancelling Headset',     1450000,  5, TRUE),
 ('LP-007', 'Laptop Stand Aluminium',        235000, 40, TRUE),
 ('CB-008', 'HDMI Cable 2m',                  85000,150, TRUE),
 ('SD-009', 'NVMe SSD 1TB',                 1350000, 18, TRUE),
 ('RM-010', 'DDR4 RAM 16GB',                 780000,  0, TRUE),
 ('PW-011', 'Power Bank 20000mAh',           495000, 30, TRUE),
 ('SP-012', 'Bluetooth Speaker',             725000, 14, TRUE),
 ('DS-013', 'Docking Station (discontinued)',2100000, 3, FALSE),
 ('UP-014', 'UPS 650VA',                    1250000, 10, TRUE);

INSERT INTO stock_movements (product_id, type, qty, note, created_at)
SELECT id, 'in', stock, 'Opening stock', now() - interval '10 days'
FROM products WHERE stock > 0;

INSERT INTO sales_orders (customer, status, total, created_at)
VALUES ('Budi Santoso', 'CREATED', 1975000, now() - interval '3 days');
INSERT INTO sales_order_items (order_id, sku, qty, unit_price) VALUES
 (1, 'KB-001', 2, 850000),
 (1, 'MS-002', 1, 275000);
UPDATE products SET stock = stock - 2 WHERE sku = 'KB-001';
UPDATE products SET stock = stock - 1 WHERE sku = 'MS-002';
INSERT INTO stock_movements (product_id, type, qty, note, created_at)
SELECT id, 'out', CASE sku WHEN 'KB-001' THEN 2 ELSE 1 END, 'Sales order #1', now() - interval '3 days'
FROM products WHERE sku IN ('KB-001', 'MS-002');

INSERT INTO sales_orders (customer, status, total, created_at, cancelled_at)
VALUES ('Sari Wulandari', 'CANCELLED', 3420000, now() - interval '2 days', now() - interval '1 day');
INSERT INTO sales_order_items (order_id, sku, qty, unit_price) VALUES
 (2, 'MN-003', 1, 3250000),
 (2, 'CB-008', 2,   85000);
INSERT INTO stock_movements (product_id, type, qty, note, created_at)
SELECT id, 'out', CASE sku WHEN 'MN-003' THEN 1 ELSE 2 END, 'Sales order #2', now() - interval '2 days'
FROM products WHERE sku IN ('MN-003', 'CB-008');
INSERT INTO stock_movements (product_id, type, qty, note, created_at)
SELECT id, 'in', CASE sku WHEN 'MN-003' THEN 1 ELSE 2 END, 'Sales order #2 cancelled', now() - interval '1 day'
FROM products WHERE sku IN ('MN-003', 'CB-008');

INSERT INTO sales_orders (customer, status, total, created_at)
VALUES ('Toko Maju Jaya', 'CREATED', 2290000, now() - interval '2 hours');
INSERT INTO sales_order_items (order_id, sku, qty, unit_price) VALUES
 (3, 'LP-007', 4,  235000),
 (3, 'SD-009', 1, 1350000)
;
UPDATE products SET stock = stock - 4 WHERE sku = 'LP-007';
UPDATE products SET stock = stock - 1 WHERE sku = 'SD-009';
INSERT INTO stock_movements (product_id, type, qty, note, created_at)
SELECT id, 'out', CASE sku WHEN 'LP-007' THEN 4 ELSE 1 END, 'Sales order #3', now() - interval '2 hours'
FROM products WHERE sku IN ('LP-007', 'SD-009');

INSERT INTO audit_log (entity_type, entity_id, entity_label, action, details, created_at)
SELECT 'product', id, sku, 'product.created',
       jsonb_build_object('sku', sku, 'name', name, 'price', price, 'is_active', is_active), now() - interval '10 days'
FROM products;
INSERT INTO audit_log (entity_type, entity_id, entity_label, action, details, created_at) VALUES
 ('order', 1, 'Order #1', 'order.created', '{"customer":"Budi Santoso","total":1975000,"items":[{"sku":"KB-001","qty":2,"unit_price":850000},{"sku":"MS-002","qty":1,"unit_price":275000}]}', now() - interval '3 days'),
 ('order', 2, 'Order #2', 'order.created', '{"customer":"Sari Wulandari","total":3420000,"items":[{"sku":"MN-003","qty":1,"unit_price":3250000},{"sku":"CB-008","qty":2,"unit_price":85000}]}', now() - interval '2 days'),
 ('order', 2, 'Order #2', 'order.cancelled', '{"customer":"Sari Wulandari","items":[{"sku":"MN-003","qty":1},{"sku":"CB-008","qty":2}]}', now() - interval '1 day'),
 ('order', 3, 'Order #3', 'order.created', '{"customer":"Toko Maju Jaya","total":2290000,"items":[{"sku":"LP-007","qty":4,"unit_price":235000},{"sku":"SD-009","qty":1,"unit_price":1350000}]}', now() - interval '2 hours');

SELECT setval('sales_orders_id_seq', (SELECT max(id) FROM sales_orders));
