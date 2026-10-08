ALTER TABLE memories ADD COLUMN embedding BLOB;
ALTER TABLE memories ADD COLUMN embedding_model TEXT NOT NULL DEFAULT '';
