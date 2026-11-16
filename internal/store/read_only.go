package store

// ReadOnly reports the connection policy; it does not weaken DB-side protection.
func (s *Store) ReadOnly() bool { return s.readOnly }
