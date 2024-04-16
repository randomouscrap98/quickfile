package quickfile

import (
	"fmt"
	"time"
)

// Get the (current) total file size for this user
func GetUserFileSize(user string, config *Config) (int64, error) {
	db, err := config.OpenDb()
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var length int64
	err = db.QueryRow(
		"SELECT IFNULL(SUM(length), 0) FROM meta WHERE account = ? AND (expire IS NULL OR expire > ?)",
		user, time.Now(),
	).Scan(&length)
	return length, err
}

// Get the (current) total file size overall (all files)
func GetTotalFileSize(config *Config) (int64, error) {
	db, err := config.OpenDb()
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var length int64
	err = db.QueryRow(
		"SELECT IFNULL(SUM(length), 0) FROM meta WHERE (expire IS NULL OR expire > ?)",
		time.Now(),
	).Scan(&length)
	return length, err
}

// Lookup a set of files by id. Get all information about them.
func GetFilesById(ids []int64, config *Config) (map[int64]*UploadFile, error) {
	db, err := config.OpenDb()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	result := make(map[int64]*UploadFile)

	placeholder := sliceToPlaceholder(ids)
	anyIds := sliceToAny(ids)

	// Go get the main data
	rows, err := db.Query(fmt.Sprintf("SELECT * FROM meta WHERE fid IN (%s)", placeholder), anyIds...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		thisFile := UploadFile{
			Tags: make([]string, 0, 5),
		}
		err := rows.Scan(&thisFile.ID, &thisFile.Name, &thisFile.Account, &thisFile.Mime,
			&thisFile.Date, &thisFile.Expire, &thisFile.Length)
		if err != nil {
			return nil, err
		}
		result[thisFile.ID] = &thisFile
	}

	rows, err = db.Query(fmt.Sprintf("SELECT fid,tag FROM tags WHERE fid IN (%s)", placeholder), anyIds...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var fid int64
		var tag string
		err := rows.Scan(&fid, &tag)
		if err != nil {
			return nil, err
		}
		result[fid].Tags = append(result[fid].Tags, tag)
	}

	return result, err
}

// Get a single file by id
func GetFileById(id int64, config *Config) (*UploadFile, error) {
	results, err := GetFilesById([]int64{id}, config)
	if err != nil {
		return nil, err
	}
	result, ok := results[id]
	if !ok {
		return nil, fmt.Errorf("not found: %d", id)
	}
	return result, nil
}

// Return file ids ordered by newest first
func GetPaginatedFiles(page int, perpage int, config *Config) ([]int64, error) {
	skip := perpage * page
	db, err := config.OpenDb()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	result := make([]int64, 0, perpage)
	rows, err := db.Query(
		"SELECT fid FROM meta WHERE (expire IS NULL OR expire > ?) ORDER BY fid DESC LIMIT ? OFFSET ?",
		time.Now(), perpage, skip,
	)
	defer rows.Close()

	for rows.Next() {
		var fid int64
		err := rows.Scan(&fid)
		if err != nil {
			return nil, err
		}
		result = append(result, fid)
	}

	return result, nil
}
