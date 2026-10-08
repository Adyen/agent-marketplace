package sqlite

import "database/sql"

func (s *Store) PoolStats() sql.DBStats {
	if s.db == nil {
		return sql.DBStats{}
	}
	return s.db.Stats()
}
