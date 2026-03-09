-- Add status and billing_details columns to tenants table
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS status varchar(20) DEFAULT 'published';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS billing_details jsonb DEFAULT '{}'::jsonb;

-- Add missing columns to customers table
ALTER TABLE customers ADD COLUMN IF NOT EXISTS environment_id varchar(50);
ALTER TABLE customers ADD COLUMN IF NOT EXISTS metadata jsonb DEFAULT '{}'::jsonb;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS address_line1 varchar(255);
ALTER TABLE customers ADD COLUMN IF NOT EXISTS address_line2 varchar(255);
ALTER TABLE customers ADD COLUMN IF NOT EXISTS address_city varchar(100);
ALTER TABLE customers ADD COLUMN IF NOT EXISTS address_state varchar(100);
ALTER TABLE customers ADD COLUMN IF NOT EXISTS address_postal_code varchar(20);
ALTER TABLE customers ADD COLUMN IF NOT EXISTS address_country varchar(2);
ALTER TABLE customers ADD COLUMN IF NOT EXISTS parent_customer_id varchar(50);
