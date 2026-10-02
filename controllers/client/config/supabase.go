package config

import (
	"database/sql"
	"log"
	"os"
	"sync"

	_ "github.com/lib/pq"
)

var (
	db   *sql.DB
	once sync.Once
)

func InitDB() {
	once.Do(func() {
		var err error
		db, err = sql.Open("postgres", os.Getenv("DATABASE_URL"))
		if err != nil {
			log.Fatalf("erro ao abrir DB: %v", err)
		}

		if err = db.Ping(); err != nil {
			log.Fatalf("erro ao conectar no DB: %v", err)
		}

		log.Println("✅ Banco de dados conectado")
	})
}

func GetDB() *sql.DB {
	if db == nil {
		log.Fatal("GetDB chamado antes de InitDB()")
	}
	return db
}