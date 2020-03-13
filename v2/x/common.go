package x

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"cloud.google.com/go/civil"
	"github.com/rocketlaunchr/dbq/v2"
)

type res struct{}

func (*res) LastInsertId() (int64, error) {
	return 0, nil
}

func (*res) RowsAffected() (int64, error) {
	return 0, nil
}

// BulkUpdateOptions is used to configure the BulkUpdate function.
type BulkUpdateOptions struct {

	// Table sets the table name.
	Table string

	// Columns sets the columns that require updating.
	Columns []string

	// PrimaryKey sets the column name which is the primary key for the purposes of how
	// BulkUpdate works.
	PrimaryKey string

	// StmtSuffix appends additional sql content to the end of the generated sql statement.
	StmtSuffix string

	// DBType sets the database being used. The default is MySQL.
	DBType dbq.Database
}

// BulkUpdate is used to update multiple rows in a table without a transaction.
//
// updateData's key must be the primary key's value in the table.
//
// updateData's value is a slice containing the new values for each column. A nil value is acceptable.
// The slice must be the same length as the number of columns being updated.
//
// NOTE: You should perform benchmarks to determine if using a transactions and multiple single-row updates is more efficient.
//
// Example:
//
//  opts := x.BulkUpdateOptions{
//     Table:      "tablename",
//     Columns:    []string{"name", "age"},
//     PrimaryKey: "id",
//  }
//
//  updateData := map[interface{}][]interface{}{
//     1: []interface{}{"rabbit", 5},
//     2: []interface{}{"cat", 8},
//  }
//
//  x.BulkUpdate(ctx, db, updateData, opts)
//
func BulkUpdate(ctx context.Context, db dbq.ExecContexter, updateData map[interface{}][]interface{}, opts BulkUpdateOptions) (sql.Result, error) {

	if opts.Table == "" || len(opts.Columns) == 0 {
		return nil, errors.New("no table name or column name(s) provided")
	}

	if len(updateData) == 0 {
		return &res{}, nil
	}

	if opts.PrimaryKey == "" {
		return nil, errors.New("primary key column in database table needs to be specified")
	}

	queryArgs := []interface{}{}

	sqlUpdate := fmt.Sprintf("UPDATE %s SET\n", opts.Table)
	sqlUpdateBack := "\nWHERE " + opts.PrimaryKey + " IN %s"

	// Generate query
	var primaryKeys []interface{} // for final WHERE IN

	var phIdx int

	for j, field := range opts.Columns {

		eachSet := fmt.Sprintf("%s = CASE\n", field)

		for primaryKey, val := range updateData {
			if j == 0 {
				primaryKeys = append(primaryKeys, primaryKey)
			}

			if val[j] == nil {
				if opts.DBType == dbq.PostgreSQL {
					eachSet = eachSet + fmt.Sprintf("\tWHEN %v = $%d THEN NULL\n", opts.PrimaryKey, phIdx+1)
					phIdx++
				} else {
					eachSet = eachSet + fmt.Sprintf("\tWHEN %v = ? THEN NULL\n", opts.PrimaryKey)
				}

				queryArgs = append(queryArgs, primaryKey)
			} else {

				var v interface{}

				if reflect.ValueOf(val[j]).Kind() == reflect.Ptr {
					if reflect.ValueOf(val[j]).IsNil() {
						v = nil
					} else {
						v = reflect.ValueOf(val[j]).Elem().Interface()
					}
				} else {
					v = val[j]
				}

				if opts.DBType == dbq.PostgreSQL {

					var colType string
					if v != nil {
						switch v.(type) {
						case uint, int, *uint, *int:
							colType = "INT"
						case uint8, uint16, uint32, uint64, *uint8, *uint16, *uint32, *uint64:
							colType = "INT"
						case int8, int16, int32, int64, *int8, *int16, *int32, *int64:
							colType = "INT"
						case string, *string:
							colType = "VARCHAR"
						case float32, *float32, float64, *float64:
							colType = "NUMERIC"
						case bool, *bool:
							colType = "BOOLEAN"
						case civil.Date, *civil.Date:
							// FIX UP
						case civil.DateTime, *civil.DateTime:
							// FIX UP
						case civil.Time, *civil.Time:
							// FIX UP
						case time.Time, *time.Time:
							colType = "TIMESTAMP"
						default:
							colType = "TEXT"
						}
					}

					eachSet = eachSet + fmt.Sprintf("WHEN %v = $%d THEN $%d::%s\n", opts.PrimaryKey, phIdx+1, phIdx+2, colType)
					phIdx += 2
				} else {
					eachSet = eachSet + fmt.Sprintf("WHEN %v = ? THEN ?\n", opts.PrimaryKey)
				}
				queryArgs = append(queryArgs, primaryKey, v)
			}
		}

		eachSet = eachSet + "END,\n"

		sqlUpdate = fmt.Sprintf("%s %s", sqlUpdate, eachSet)
	}
	sqlUpdate = strings.TrimSuffix(sqlUpdate, ",\n")

	stmt := sqlUpdate + fmt.Sprintf(sqlUpdateBack, dbq.Ph(len(primaryKeys), 1, phIdx, opts.DBType))

	// Add suffix
	if opts.StmtSuffix != "" {
		stmt = stmt + " " + opts.StmtSuffix
	}

	fmt.Println(stmt)

	queryArgs = append(queryArgs, primaryKeys...)

	return dbq.E(ctx, db, stmt, nil, queryArgs...)
}
