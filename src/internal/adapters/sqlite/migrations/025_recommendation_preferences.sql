CREATE TABLE IF NOT EXISTS recommendation_preferences(source TEXT PRIMARY KEY,enabled INTEGER NOT NULL CHECK(enabled IN (0,1)));
