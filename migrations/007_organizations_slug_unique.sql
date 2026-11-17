-- 007_organizations_slug_unique.sql
-- Garante um indice unico de coluna unica em organizations.slug para o fluxo
-- de sufixo (INSERT ... ON CONFLICT (slug) DO NOTHING) usado no onboarding.

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
      FROM pg_index i
      JOIN pg_attribute a
        ON a.attrelid = i.indrelid
       AND a.attnum = i.indkey[0]
     WHERE i.indrelid = 'organizations'::regclass
       AND i.indisunique
       AND i.indnkeyatts = 1
       AND i.indpred IS NULL
       AND a.attname = 'slug'
  ) THEN
    CREATE UNIQUE INDEX organizations_slug_unique_idx
      ON organizations (slug);
  END IF;
END
$$;
