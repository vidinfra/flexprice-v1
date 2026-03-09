-- Allow entity_id-based pricing without legacy plan_id writes
ALTER TABLE public.prices
    ALTER COLUMN plan_id DROP NOT NULL;

-- Backfill plan_id for plan-scoped prices when possible (only if entity_type column exists)
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'prices' AND column_name = 'entity_type') THEN
        UPDATE public.prices
        SET plan_id = entity_id
        WHERE plan_id IS NULL
          AND entity_type = 'PLAN'
          AND entity_id IS NOT NULL;
    END IF;
END $$;
