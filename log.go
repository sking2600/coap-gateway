package main

import "go.uber.org/zap"

var logger zap.Logger
var log *zap.SugaredLogger

func init() {
	config := zap.NewDevelopmentConfig()
	logger, err := config.Build()
	if err != nil {
		panic("Unable to create logger")
	}
	log = logger.Sugar()
}
