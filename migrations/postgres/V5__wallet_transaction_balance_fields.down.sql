-- Remove balance_before and balance_after columns from wallet_transactions table

ALTER TABLE public.wallet_transactions
    DROP COLUMN IF EXISTS balance_before;

ALTER TABLE public.wallet_transactions
    DROP COLUMN IF EXISTS balance_after;
