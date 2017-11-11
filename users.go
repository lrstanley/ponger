package main

import "github.com/asdine/storm"

type UserSettings struct {
	ID             string `storm:"id"`
	ChecksDisabled bool
}

func GetUserSettings(user string) (settings *UserSettings) {
	db := newUserDB()
	defer db.Close()

	settings = &UserSettings{ID: user}

	err := db.One("ID", user, settings)
	if err != nil && err != storm.ErrNotFound {
		panic(err)
	}

	return settings
}

func SetUserSettings(settings *UserSettings) {
	db := newUserDB()
	defer db.Close()

	err := db.Save(settings)
	if err == storm.ErrAlreadyExists {
		err = db.Update(settings)
	}
	if err != nil {
		panic(err)
	}
}

func newUserDB() *storm.DB {
	db, err := storm.Open(flags.UserDB)
	if err != nil {
		panic(err)
	}

	return db
}
