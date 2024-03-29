package basemodel

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/jinzhu/gorm"
)

// used constants
const (
	MysqlAdapter    string = "mysql_adapter"
	PostgresAdapter string = "postgres_adapter"
)

type (
	// DBConfig contains db configs
	DBConfig struct {
		Adapter        string
		Host           string
		Port           string
		Username       string
		Password       string
		Table          string
		Timezone       string
		Maxlifetime    int
		IdleConnection int
		OpenConnection int
		SSL            string
		Logmode        bool
	}

	// BaseModel will be used as foundation of all models
	BaseModel struct {
		ID          uint64    `json:"id" sql:"AUTO_INCREMENT" gorm:"primary_key,column:id"`
		CreatedTime time.Time `json:"created_time" gorm:"column:created_time" sql:"DEFAULT:current_timestamp"`
		UpdatedTime time.Time `json:"updated_time" gorm:"column:updated_time" sql:"DEFAULT:current_timestamp"`
	}

	// DBFunc gorm trx function
	DBFunc func(tx *gorm.DB) error

	// PagedFindResult pagination format
	PagedFindResult struct {
		TotalData   int         `json:"total_data"`   // matched datas
		Rows        int         `json:"rows"`         // shown datas per page
		CurrentPage int         `json:"current_page"` // current page
		LastPage    int         `json:"last_page"`
		From        int         `json:"from"` // offset, starting index of data shown in current page
		To          int         `json:"to"`   // last index of data shown in current page
		Data        interface{} `json:"data"`
	}

	// CompareFilter filter used for comparing 2 values
	CompareFilter struct {
		Value1 interface{} `json:"value1"`
		Value2 interface{} `json:"value2"`
	}
)

// DB is a connected db instance
var DB *gorm.DB

// Start set basemodel config
func Start(conf DBConfig) {
	switch conf.Adapter {
	case MysqlAdapter:
		connectionString := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true", conf.Username, conf.Password, conf.Host, conf.Port, conf.Table)
		if err := DBConnect("mysql", connectionString, conf); err != nil {
			panic(err)
		}
	case PostgresAdapter:
		connectionString := fmt.Sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=%s", conf.Username, conf.Password, conf.Host, conf.Port, conf.Table, conf.SSL)
		if err := DBConnect("postgres", connectionString, conf); err != nil {
			panic(err)
		}
	}
}

// Close DB connection
func Close() {
	DB.Close()
}

// DBConnect connects to db instance
func DBConnect(gormAdapter string, connectionString string, conf DBConfig) (err error) {
	DB, err = gorm.Open(gormAdapter, connectionString)
	if err != nil {
		return err
	}
	if err = DB.DB().Ping(); err != nil {
		return err
	}

	DB.LogMode(conf.Logmode)

	DB.Exec(fmt.Sprintf("SET TIMEZONE TO '%s'", conf.Timezone))
	DB.DB().SetConnMaxLifetime(time.Second * time.Duration(conf.Maxlifetime))
	DB.DB().SetMaxIdleConns(conf.IdleConnection)
	DB.DB().SetMaxOpenConns(conf.OpenConnection)

	return nil
}

// WithinTransaction sql execute helper
func WithinTransaction(fn DBFunc) (err error) {
	tx := DB.Begin()
	defer tx.Commit()
	err = fn(tx)

	return err
}

// Create creates row based on model
func Create(i interface{}) error {
	return WithinTransaction(func(tx *gorm.DB) (err error) {
		if !DB.NewRecord(i) {
			return fmt.Errorf("cannot create row. not a new record")
		}
		if err = tx.Create(i).Error; err != nil {
			tx.Rollback()
			return err
		}
		return err
	})
}

// Save updates row based on model
func Save(i interface{}) error {
	return WithinTransaction(func(tx *gorm.DB) (err error) {
		if DB.NewRecord(i) {
			return fmt.Errorf("cannot save row. it is a new record")
		}
		if err = tx.Save(i).Error; err != nil {
			tx.Rollback()
			return err
		}
		return err
	})
}

// Delete removes row in db based on model.
func Delete(i interface{}) error {
	return WithinTransaction(func(tx *gorm.DB) (err error) {
		if err = tx.Delete(i).Error; err != nil {
			tx.Rollback()
			return err
		}
		return err
	})
}

// FirstOrCreate gets first matched record, or create a new one
func FirstOrCreate(i interface{}) error {
	return WithinTransaction(func(tx *gorm.DB) (err error) {
		if err = tx.FirstOrCreate(i).Error; err != nil {
			tx.Rollback()
			return err
		}
		return err
	})
}

// FindbyID finds row by id.
func FindbyID(i interface{}, id int) (err error) {
	return WithinTransaction(func(tx *gorm.DB) error {
		if err = tx.Last(i, id).Error; err != nil {
			tx.Rollback()
			return err
		}
		return err
	})
}

// FindFilter finds by filter. limit 0 to find all
func FindFilter(i interface{}, order []string, sort []string, limit int, offset int, filter interface{}) (interface{}, error) {
	query := DB // clone db connection

	conditionQuery(query, filter)
	orderSortQuery(query, order, sort)

	if limit > 0 {
		query = query.Limit(limit)
	}

	if offset > 0 {
		query = query.Offset(offset)
	}

	err := query.Find(i).Error

	return i, err
}

// PagedFindFilter same with FindFilter but with pagination
func PagedFindFilter(i interface{}, page int, rows int, order []string, sort []string, filter interface{}) (result PagedFindResult, err error) {
	if page <= 0 {
		page = 1
	}
	if rows <= 0 {
		rows = 25
	}

	query := DB

	conditionQuery(query, filter)
	orderSortQuery(query, order, sort)

	temp := query
	var totalRows int

	temp.Find(i).Count(&totalRows)

	offset := (page * rows) - rows
	lastPage := int(math.Ceil(float64(totalRows) / float64(rows)))

	query.Limit(rows).Offset(offset).Find(i)

	result = PagedFindResult{
		TotalData:   totalRows,
		Rows:        rows,
		CurrentPage: page,
		LastPage:    lastPage,
		From:        offset + 1,
		To:          offset + rows,
		Data:        i,
	}

	return result, err
}

func conditionQuery(query *gorm.DB, filter interface{}) {
	refFilter := reflect.ValueOf(filter).Elem()
	refType := refFilter.Type()
	for x := 0; x < refFilter.NumField(); x++ {
		field := refFilter.Field(x)
		if field.Interface() != "" {
			switch refType.Field(x).Tag.Get("condition") {
			default:
				query = query.Where(fmt.Sprintf("%s = ?", refType.Field(x).Tag.Get("json")), field.Interface())
			case "LIKE":
				query = query.Where(fmt.Sprintf("LOWER(%s) %s ?", refType.Field(x).Tag.Get("json"), refType.Field(x).Tag.Get("condition")), "%"+strings.ToLower(field.Interface().(string))+"%")
			case "BETWEEN":
				if values, ok := field.Interface().(CompareFilter); ok && values.Value1 != "" {
					query = query.Where(fmt.Sprintf("%s %s ? %s ?", refType.Field(x).Tag.Get("json"), refType.Field(x).Tag.Get("condition"), "AND"), values.Value1, values.Value2)
				}
			case "OR":
				var e []string
				for _, v := range field.Interface().([]string) {
					e = append(e, refType.Field(x).Tag.Get("json")+" = '"+v)
				}
				query = query.Where(strings.Join(e, " OR "))
			}
		}
	}
}

func orderSortQuery(query *gorm.DB, order []string, sort []string) {
	for k, v := range order {
		q := v
		if len(sort) > k {
			value := sort[k]
			if strings.ToUpper(value) == "ASC" || strings.ToUpper(value) == "DESC" {
				q = v + " " + strings.ToUpper(value)
			}
		}
		query = query.Order(q)
	}
}
