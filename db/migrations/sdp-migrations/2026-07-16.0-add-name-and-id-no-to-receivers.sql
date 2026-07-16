-- Add name and id_no columns to receivers table

-- +migrate Up
ALTER TABLE receivers ADD COLUMN name VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE receivers ADD COLUMN id_no VARCHAR(8);

-- +migrate Down
ALTER TABLE receivers DROP COLUMN IF EXISTS name;
ALTER TABLE receivers DROP COLUMN IF EXISTS id_no;