-- name: GetStoreSettings :one
SELECT * FROM store_settings
WHERE singleton_key = TRUE
LIMIT 1;

-- name: UpdateStoreSettings :one
UPDATE store_settings
SET
    store_name = $1,
    description = $2,
    phone = $3,
    whatsapp = $4,
    email = $5,
    address_line = $6,
    opening_hours = $7,
    accepted_payment_methods = $8,
    manual_status = $9,
    allow_scheduling = $10,
    pix_reservation_ttl_minutes = $11,
    auto_hide_out_of_stock = $12,
    updated_at = NOW()
WHERE singleton_key = TRUE
RETURNING *;

-- name: CreateCategory :one
INSERT INTO categories (id, name, slug, sort_order, active)
VALUES (
    $1,
    $2,
    LOWER(TRIM(sqlc.arg('slug')::text)),
    $3,
    $4
)
RETURNING *;

-- name: GetCategoryByID :one
SELECT * FROM categories
WHERE id = $1;

-- name: CountCategoriesByFilter :one
SELECT COUNT(*) FROM categories
WHERE (sqlc.narg('active')::boolean IS NULL OR active = sqlc.narg('active')::boolean);

-- name: ListCategories :many
SELECT * FROM categories
WHERE (sqlc.narg('active')::boolean IS NULL OR active = sqlc.narg('active')::boolean)
ORDER BY sort_order ASC, name ASC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: ListActiveCategories :many
SELECT * FROM categories
WHERE active = TRUE
ORDER BY sort_order ASC, name ASC;

-- name: UpdateCategory :one
UPDATE categories
SET
    name = $2,
    slug = LOWER(TRIM(sqlc.arg('slug')::text)),
    sort_order = $3,
    active = $4,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteCategory :execrows
DELETE FROM categories
WHERE id = $1;

-- name: CreateProduct :one
INSERT INTO products (
    id,
    category_id,
    name,
    slug,
    description,
    service_type,
    prep_time_minutes,
    active,
    available_online,
    featured,
    sort_order
)
VALUES (
    $1,
    $2,
    $3,
    LOWER(TRIM(sqlc.arg('slug')::text)),
    $4,
    $5,
    $6,
    $7,
    $8,
    $9,
    $10
)
RETURNING *;

-- name: GetProductByID :one
SELECT * FROM products
WHERE id = $1;

-- name: CountProductsByFilter :one
SELECT COUNT(*) FROM products p
WHERE (sqlc.narg('category_id')::uuid IS NULL OR p.category_id = sqlc.narg('category_id')::uuid)
  AND (sqlc.narg('service_type')::text IS NULL OR p.service_type = sqlc.narg('service_type')::text)
  AND (sqlc.narg('active')::boolean IS NULL OR p.active = sqlc.narg('active')::boolean)
  AND (sqlc.narg('available_online')::boolean IS NULL OR p.available_online = sqlc.narg('available_online')::boolean)
  AND (
      sqlc.narg('search')::text IS NULL
      OR p.name ILIKE '%' || sqlc.narg('search')::text || '%' ESCAPE '\'
      OR p.description ILIKE '%' || sqlc.narg('search')::text || '%' ESCAPE '\'
  );

-- name: ListProducts :many
SELECT p.* FROM products p
WHERE (sqlc.narg('category_id')::uuid IS NULL OR p.category_id = sqlc.narg('category_id')::uuid)
  AND (sqlc.narg('service_type')::text IS NULL OR p.service_type = sqlc.narg('service_type')::text)
  AND (sqlc.narg('active')::boolean IS NULL OR p.active = sqlc.narg('active')::boolean)
  AND (sqlc.narg('available_online')::boolean IS NULL OR p.available_online = sqlc.narg('available_online')::boolean)
  AND (
      sqlc.narg('search')::text IS NULL
      OR p.name ILIKE '%' || sqlc.narg('search')::text || '%' ESCAPE '\'
      OR p.description ILIKE '%' || sqlc.narg('search')::text || '%' ESCAPE '\'
  )
ORDER BY p.sort_order ASC, p.name ASC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: ListPublicMenuProducts :many
SELECT
    p.id,
    p.category_id,
    c.name AS category_name,
    c.slug AS category_slug,
    p.name,
    p.slug,
    p.description,
    p.image_url,
    p.service_type,
    p.prep_time_minutes,
    p.featured,
    p.sort_order
FROM products p
JOIN categories c ON c.id = p.category_id
WHERE p.active = TRUE
  AND p.available_online = TRUE
  AND c.active = TRUE
  AND (sqlc.narg('category_slug')::text IS NULL OR LOWER(c.slug) = LOWER(TRIM(sqlc.narg('category_slug')::text)))
  AND (sqlc.narg('service_type')::text IS NULL OR p.service_type = sqlc.narg('service_type')::text)
  AND (sqlc.narg('featured_only')::boolean IS NULL OR sqlc.narg('featured_only')::boolean = FALSE OR p.featured = TRUE)
  AND (
      sqlc.narg('search')::text IS NULL
      OR p.name ILIKE '%' || sqlc.narg('search')::text || '%' ESCAPE '\'
      OR p.description ILIKE '%' || sqlc.narg('search')::text || '%' ESCAPE '\'
  )
ORDER BY c.sort_order ASC, c.name ASC, p.sort_order ASC, p.name ASC;

-- name: GetPublicProductByID :one
SELECT
    p.id,
    p.category_id,
    c.name AS category_name,
    c.slug AS category_slug,
    p.name,
    p.slug,
    p.description,
    p.image_url,
    p.service_type,
    p.prep_time_minutes,
    p.featured,
    p.sort_order
FROM products p
JOIN categories c ON c.id = p.category_id
WHERE p.id = $1
  AND p.active = TRUE
  AND p.available_online = TRUE
  AND c.active = TRUE;

-- name: GetPublicProductBySlug :one
SELECT
    p.id,
    p.category_id,
    c.name AS category_name,
    c.slug AS category_slug,
    p.name,
    p.slug,
    p.description,
    p.image_url,
    p.service_type,
    p.prep_time_minutes,
    p.featured,
    p.sort_order
FROM products p
JOIN categories c ON c.id = p.category_id
WHERE LOWER(p.slug) = LOWER(TRIM(sqlc.arg('slug')::text))
  AND p.active = TRUE
  AND p.available_online = TRUE
  AND c.active = TRUE;

-- name: UpdateProduct :one
UPDATE products
SET
    category_id = $2,
    name = $3,
    slug = LOWER(TRIM(sqlc.arg('slug')::text)),
    description = $4,
    service_type = $5,
    prep_time_minutes = $6,
    active = $7,
    available_online = $8,
    featured = $9,
    sort_order = $10,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateProductImage :one
UPDATE products
SET
    image_url = $2,
    image_key = $3,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteProduct :execrows
DELETE FROM products
WHERE id = $1;

-- name: CreateProductVariant :one
INSERT INTO product_variants (
    id,
    product_id,
    name,
    sku,
    price_cents,
    current_stock,
    reserved_stock,
    min_stock,
    sort_order,
    active
)
VALUES (
    $1,
    $2,
    $3,
    NULLIF(TRIM(sqlc.narg('sku')::text), ''),
    $4,
    $5,
    $6,
    $7,
    $8,
    $9
)
RETURNING *;

-- name: GetProductVariantByID :one
SELECT * FROM product_variants
WHERE id = $1 AND product_id = $2;

-- name: ListVariantsByProductIDs :many
SELECT * FROM product_variants
WHERE product_id = ANY(sqlc.arg('product_ids')::uuid[])
ORDER BY sort_order ASC, name ASC, id ASC;

-- name: ListVariantsByProductIDForUpdate :many
SELECT * FROM product_variants
WHERE product_id = $1
ORDER BY sort_order ASC, name ASC, id ASC
FOR UPDATE;

-- name: UpdateProductVariant :one
UPDATE product_variants
SET
    name = $3,
    sku = NULLIF(TRIM(sqlc.narg('sku')::text), ''),
    price_cents = $4,
    min_stock = $5,
    sort_order = $6,
    active = $7,
    updated_at = NOW()
WHERE id = $1 AND product_id = $2
RETURNING *;

-- name: DeleteProductVariant :execrows
DELETE FROM product_variants
WHERE id = $1 AND product_id = $2;
