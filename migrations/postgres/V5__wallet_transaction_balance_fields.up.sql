-- Add balance_before and balance_after columns to wallet_transactions table
-- These store the currency-equivalent balance snapshots alongside credit balance snapshots

ALTER TABLE public.wallet_transactions
    ADD COLUMN IF NOT EXISTS balance_before NUMERIC(20,9) NOT NULL DEFAULT 0;

ALTER TABLE public.wallet_transactions
    ADD COLUMN IF NOT EXISTS balance_after NUMERIC(20,9) NOT NULL DEFAULT 0;
