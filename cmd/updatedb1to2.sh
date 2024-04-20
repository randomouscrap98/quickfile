#!/bin/sh

sqlite3 uploads.db <<EOF
ALTER TABLE meta ADD unlisted TEXT NOT NULL DEFAULT "";
UPDATE sysvalues SET value='2' WHERE "key"='version';
EOF

echo "Done"
