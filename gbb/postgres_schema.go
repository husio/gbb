package gbb

import (
	"database/sql"
	"fmt"
	"strings"
)

func EnsureSchema(db *sql.DB) error {
	for i, query := range strings.Split(schema, ";\n\n") {
		if _, err := db.Exec(query); err != nil {
			begin := query
			if len(begin) > 60 {
				begin = begin[:58] + ".."
			}
			return fmt.Errorf("cannot execute %d query (%q): %s", i, begin, err)
		}
	}
	return nil
}

const schema = `

CREATE TABLE IF NOT EXISTS
users (
	user_id SERIAL PRIMARY KEY,
	password TEXT NOT NULL,
	name TEXT NOT NULL,
	scopes SMALLINT NOT NULL DEFAULT 0
);


ALTER TABLE users ADD COLUMN IF NOT EXISTS scopes SMALLINT NOT NULL DEFAULT 0;


CREATE TABLE IF NOT EXISTS
topics (
	topic_id SERIAL PRIMARY KEY,
	subject TEXT NOT NULL,
	created TIMESTAMPTZ NOT NULL,
	author_id INTEGER NOT NULL REFERENCES users(user_id),
	views_count INTEGER NOT NULL default 0 CHECK (views_count >= 0),
	comments_count INTEGER NOT NULL default 1 CHECK (comments_count >= 0),
	latest_comment TIMESTAMPTZ NOT NULL DEFAULT now()
);


CREATE TABLE IF NOT EXISTS
comments (
	comment_id SERIAL PRIMARY KEY,
	topic_id INTEGER NOT NULL REFERENCES topics(topic_id),
	content TEXT NOT NULL,
	created TIMESTAMPTZ NOT NULL,
	author_id INTEGER NOT NULL REFERENCES users(user_id)
);

CREATE INDEX IF NOT EXISTS comments_created_idx ON comments(created);

CREATE INDEX IF NOT EXISTS topics_created_idx ON topics(latest_comment);
`
