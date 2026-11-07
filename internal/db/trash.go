package db

import "database/sql"

// Trashed is the trash's record of something trashed through the app.
type Trashed struct {
	Original string
	Size     int64
}

func (c conn) AddTrashed(name, original string, size int64) error {
	_, err := c.q.Exec(`INSERT INTO trash (name, original, size) VALUES (?, ?, ?)`, name, original, size)
	return err
}

// TrashRecords is every record, by the item's name in the trash.
func (c conn) TrashRecords() (map[string]Trashed, error) {
	records := map[string]Trashed{}
	rows, err := c.q.Query(`SELECT name, original, size FROM trash`)
	_, err = collect(rows, err, func(rows *sql.Rows) (struct{}, error) {
		var name string
		var record Trashed
		err := rows.Scan(&name, &record.Original, &record.Size)
		records[name] = record
		return struct{}{}, err
	})
	return records, err
}

// TrashedFrom is where the item called name in the trash came from, false if it has no record.
func (c conn) TrashedFrom(name string) (string, bool, error) {
	var original string
	err := c.q.QueryRow(`SELECT original FROM trash WHERE name = ?`, name).Scan(&original)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return original, err == nil, err
}

// ForgetTrashed drops the record and the tags of the item called name in the trash.
func (c conn) ForgetTrashed(name string) error {
	if _, err := c.q.Exec(`DELETE FROM trash WHERE name = ?`, name); err != nil {
		return err
	}
	return c.DropTags(".trash/" + name)
}

// ForgetAllTrashed drops every trash record, and the tags of everything in the trash.
func (c conn) ForgetAllTrashed() error {
	if _, err := c.q.Exec(`DELETE FROM trash`); err != nil {
		return err
	}
	return c.DropTags(".trash")
}
