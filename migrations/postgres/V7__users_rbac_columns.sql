-- Add RBAC columns to users table
ALTER TABLE public.users ADD COLUMN IF NOT EXISTS type VARCHAR(255) DEFAULT 'user';
ALTER TABLE public.users ADD COLUMN IF NOT EXISTS roles JSONB DEFAULT '[]';

-- Add index for RBAC queries
CREATE INDEX IF NOT EXISTS idx_user_tenant_status ON public.users (tenant_id, status, type);
