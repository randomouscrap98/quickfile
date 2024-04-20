#!/bin/sh

sqlite3 uploads.db <<EOF
ALTER TABLE meta ADD unlisted TEXT NOT NULL DEFAULT "";
UPDATE sysvalues SET value='2' WHERE "key"='version';
DROP INDEX IF EXISTS idx_meta_expire;
DROP INDEX IF EXISTS idx_meta_account_expire;
EOF

echo "Done"
