package database

import (
	"log"
	"testing"
)

var TestDB = DB{
	User:   "test",
	Pass:   "TESTpass1!",
	Schema: "testdb",
}

// Вывод некритических ошибок тестирования в консоль
func handleError(err error) {
	if err != nil {
		log.Println(err)
	}
}

func TestCreateLog(t *testing.T) {
	CreateLog("log")
	t.Log("ok")
}

func TestInitLog(t *testing.T) {
	mainLog := InitLog("log")
	log.SetOutput(mainLog)
	log.Println("testing")
	t.Log("ok")
}

func TestConnect(t *testing.T) {
	logs := OpenLogs()
	defer logs.CloseAll()
	_, err := Connect(TestDB, logs.DBLogFile)
	handleError(err)
	_, err = Connect(TestDB, logs.DBLogFile)
	handleError(err)
	t.Log("ok")
}
