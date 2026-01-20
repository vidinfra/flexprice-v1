-- Ensure plans table has defaults for fields not set by the app
ALTER TABLE public.plans
    ALTER COLUMN invoice_cadence SET DEFAULT 'ADVANCE',
    ALTER COLUMN trial_period SET DEFAULT 0;
