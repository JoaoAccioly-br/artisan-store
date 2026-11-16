-- +goose Up
CREATE TABLE store_settings (
    id UUID PRIMARY KEY,
    singleton_key BOOLEAN NOT NULL DEFAULT TRUE UNIQUE CHECK (singleton_key = TRUE),
    store_name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    phone TEXT NOT NULL DEFAULT '',
    whatsapp TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL DEFAULT '',
    address_line TEXT NOT NULL DEFAULT '',
    opening_hours JSONB NOT NULL DEFAULT '{}'::jsonb,
    accepted_payment_methods TEXT[] NOT NULL DEFAULT ARRAY[
        'pix_online',
        'cartao_entrega',
        'dinheiro_entrega',
        'pagamento_retirada'
    ]::text[],
    manual_status TEXT NOT NULL DEFAULT 'auto' CHECK (manual_status IN ('auto', 'open', 'closed')),
    allow_scheduling BOOLEAN NOT NULL DEFAULT TRUE,
    pix_reservation_ttl_minutes INTEGER NOT NULL DEFAULT 15 CHECK (pix_reservation_ttl_minutes > 0),
    auto_hide_out_of_stock BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO store_settings (
    id,
    singleton_key,
    store_name,
    description,
    phone,
    whatsapp,
    email,
    address_line,
    opening_hours,
    accepted_payment_methods,
    manual_status,
    allow_scheduling,
    pix_reservation_ttl_minutes,
    auto_hide_out_of_stock
) VALUES (
    '01930000-0000-7000-8000-000000000001',
    TRUE,
    'Artisan Store Doceria',
    'Doces artesanais feitos diariamente com ingredientes selecionados.',
    '',
    '',
    '',
    '',
    '{
        "monday":    {"open": "09:00", "close": "18:00", "closed": false},
        "tuesday":   {"open": "09:00", "close": "18:00", "closed": false},
        "wednesday": {"open": "09:00", "close": "18:00", "closed": false},
        "thursday":  {"open": "09:00", "close": "18:00", "closed": false},
        "friday":    {"open": "09:00", "close": "19:00", "closed": false},
        "saturday":  {"open": "09:00", "close": "16:00", "closed": false},
        "sunday":    {"open": "09:00", "close": "13:00", "closed": true}
    }'::jsonb,
    ARRAY['pix_online', 'cartao_entrega', 'dinheiro_entrega', 'pagamento_retirada']::text[],
    'auto',
    TRUE,
    15,
    TRUE
) ON CONFLICT (singleton_key) DO NOTHING;

CREATE TABLE categories (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_categories_slug_lower ON categories (LOWER(slug));
CREATE INDEX idx_categories_active_sort ON categories (active, sort_order, name);

CREATE TABLE products (
    id UUID PRIMARY KEY,
    category_id UUID NOT NULL REFERENCES categories(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    image_url TEXT,
    image_key TEXT,
    service_type TEXT NOT NULL CHECK (service_type IN ('pronta_entrega', 'sob_encomenda')),
    prep_time_minutes INTEGER NOT NULL DEFAULT 0 CHECK (prep_time_minutes >= 0),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    available_online BOOLEAN NOT NULL DEFAULT TRUE,
    featured BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_products_slug_lower ON products (LOWER(slug));
CREATE INDEX idx_products_category_id ON products (category_id);
CREATE INDEX idx_products_public_menu ON products (active, available_online, sort_order, name);

CREATE TABLE product_variants (
    id UUID PRIMARY KEY,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    sku TEXT,
    price_cents BIGINT NOT NULL CHECK (price_cents >= 0),
    current_stock INTEGER NOT NULL DEFAULT 0 CHECK (current_stock >= 0),
    reserved_stock INTEGER NOT NULL DEFAULT 0 CHECK (reserved_stock >= 0),
    min_stock INTEGER NOT NULL DEFAULT 0 CHECK (min_stock >= 0),
    sort_order INTEGER NOT NULL DEFAULT 0,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_product_variants_stock_balance CHECK (current_stock >= reserved_stock)
);

CREATE UNIQUE INDEX idx_product_variants_sku_lower ON product_variants (LOWER(sku)) WHERE sku IS NOT NULL;
CREATE INDEX idx_product_variants_product_id ON product_variants (product_id);

-- +goose Down
DROP TABLE IF EXISTS product_variants;
DROP TABLE IF EXISTS products;
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS store_settings;
