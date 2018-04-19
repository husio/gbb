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
categories (
	category_id SERIAL PRIMARY KEY,
	name TEXT NOT NULL
);


INSERT INTO categories VALUES (1, 'General discussion')
	ON CONFLICT DO NOTHING;


CREATE TABLE IF NOT EXISTS
topics (
	topic_id SERIAL PRIMARY KEY,
	subject TEXT NOT NULL,
	created TIMESTAMPTZ NOT NULL,
	author_id INTEGER NOT NULL REFERENCES users(user_id),
	views_count INTEGER NOT NULL default 0 CHECK (views_count >= 0),
	comments_count INTEGER NOT NULL default 1 CHECK (comments_count >= 0),
	latest_comment TIMESTAMPTZ NOT NULL DEFAULT now(),
	category_id INTEGER NOT NULL REFERENCES categories(category_id)
);

ALTER TABLE topics ADD COLUMN IF NOT EXISTS
	category_id INTEGER NOT NULL DEFAULT 1;


ALTER TABLE topics DROP CONSTRAINT IF EXISTS fk_topics_category_id,
	ADD CONSTRAINT fk_topics_category_id FOREIGN KEY (category_id) REFERENCES categories(category_id);


ALTER TABLE topics DROP COLUMN IF EXISTS tags;


CREATE TABLE IF NOT EXISTS
comments (
	comment_id SERIAL PRIMARY KEY,
	topic_id INTEGER NOT NULL REFERENCES topics(topic_id),
	content TEXT NOT NULL,
	created TIMESTAMPTZ NOT NULL,
	author_id INTEGER NOT NULL REFERENCES users(user_id)
);


CREATE OR REPLACE FUNCTION update_topic_on_comment_insert()
RETURNS trigger AS $$
BEGIN
	UPDATE topics SET
		latest_comment = (SELECT created FROM comments WHERE topic_id = NEW.topic_id ORDER BY created DESC LIMIT 1),
		comments_count = (SELECT COUNT(*) - 1 FROM comments WHERE topic_id = NEW.topic_id)
		WHERE topic_id = NEW.topic_id;
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;


DROP TRIGGER IF EXISTS update_topic_on_comment_insert ON comments;


CREATE OR REPLACE FUNCTION update_topic_on_comment_delete()
RETURNS trigger AS $$
DECLARE
	comments_cnt INT;
BEGIN
	comments_cnt := (SELECT COUNT(*) - 1 FROM comments WHERE topic_id = OLD.topic_id);
	IF comments_cnt < 0 THEN
		comments_cnt = 0;
	END IF;
	UPDATE topics SET
		latest_comment = COALESCE((SELECT created FROM comments WHERE topic_id = OLD.topic_id ORDER BY created DESC LIMIT 1), now()),
		comments_count = comments_cnt
		WHERE topic_id = OLD.topic_id;
	RETURN OLD;
END;
$$ LANGUAGE plpgsql;


DROP TRIGGER IF EXISTS update_topic_on_comment_delete ON comments;


CREATE TRIGGER update_topic_on_comment_insert
    AFTER INSERT ON comments
    FOR EACH ROW EXECUTE PROCEDURE update_topic_on_comment_insert();


CREATE TRIGGER update_topic_on_comment_delete
    AFTER DELETE ON comments
    FOR EACH ROW EXECUTE PROCEDURE update_topic_on_comment_delete();


CREATE INDEX IF NOT EXISTS comments_created_idx ON comments(created);

CREATE INDEX IF NOT EXISTS topics_created_idx ON topics(latest_comment);
`
