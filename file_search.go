package quickfile

import (
	"fmt"
	"time"
)

type FileStatistics struct {
	TotalSize int64
	Count     int64
}

// Retrieve file statistics for a given user. If no user is given, the global
// file statistics will be given
func GetFileStatistics(user string, config *Config) (*FileStatistics, error) {
	db, err := config.OpenDb()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var result FileStatistics
	if user == "" {
		err = db.QueryRow(
			"SELECT COUNT(*) FROM meta WHERE (expire IS NULL OR expire > ?)",
			time.Now(),
		).Scan(&result.Count)
		if err != nil {
			return nil, err
		}
		err = db.QueryRow(
			"SELECT IFNULL(SUM(length), 0) FROM meta WHERE (expire IS NULL OR expire > ?)",
			time.Now(),
		).Scan(&result.TotalSize)
		if err != nil {
			return nil, err
		}
	} else {
		err = db.QueryRow(
			"SELECT COUNT(*) FROM meta WHERE account = ? AND (expire IS NULL OR expire > ?)",
			user, time.Now(),
		).Scan(&result.Count)
		if err != nil {
			return nil, err
		}
		err = db.QueryRow(
			"SELECT IFNULL(SUM(length), 0) FROM meta WHERE account = ? AND (expire IS NULL OR expire > ?)",
			user, time.Now(),
		).Scan(&result.TotalSize)
		if err != nil {
			return nil, err
		}
	}
	return &result, nil
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
	rows, err := db.Query(fmt.Sprintf("SELECT fid,name,account,mime,created,expire,length FROM meta WHERE fid IN (%s)", placeholder), anyIds...)
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
func GetPaginatedFiles(page int, config *Config) ([]int64, error) {
	perpage := config.ResultsPerPage
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
	if err != nil {
		return nil, err
	}
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
