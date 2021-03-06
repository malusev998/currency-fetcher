package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-sql-driver/mysql"
	"github.com/spf13/viper"

	"github.com/malusev998/currency"
	"github.com/malusev998/currency/fetchers"
	"github.com/malusev998/currency/storage"
)

type (
	FetchersConfig map[currency.Provider]interface{}
	StorageConfig  map[storage.Provider]interface{}
	Config         struct {
		Fetchers          []currency.Provider
		Storage           []storage.Provider
		FetchersConfig    FetchersConfig
		StorageConfig     StorageConfig
		CurrenciesToFetch []string
	}
)

func getMysqlDSN(config map[string]string) string {
	mysqlDriverConfig := mysql.NewConfig()
	mysqlDriverConfig.User = config["user"]
	mysqlDriverConfig.Passwd = config["password"]
	mysqlDriverConfig.Addr = config["addr"]
	mysqlDriverConfig.Net = "tcp"
	mysqlDriverConfig.DBName = config["db"]

	return mysqlDriverConfig.FormatDSN()
}

func getConfig(ctx context.Context) (*Config, error) {
	mysqlConfig := viper.GetStringMapString("databases.mysql")
	mongodbConfig := viper.GetStringMapString("databases.mongo")

	fetcherConfig := viper.GetStringMapString("fetchers.freecurrconversion")
	maxPerHour, err := strconv.ParseUint(fetcherConfig["maxperhour"], 10, 32)

	if err != nil {
		return nil, fmt.Errorf("error while parsing maxPerHour in fetchers.freecurrconversion: %v", err)
	}

	maxPerRequest, err := strconv.ParseUint(fetcherConfig["maxperrequest"], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("error while parsing maxPerRequest in fetchers.freecurrconversion: %v", err)
	}

	fetcher, err := currency.ConvertToProvidersFromStringSlice(viper.GetStringSlice("fetchers.fetch"))

	if err != nil {
		return nil, err
	}

	storages, err := storage.ConvertToProvidersFromStringSlice(viper.GetStringSlice("storage"))

	storageBaseConfig := storage.BaseConfig{
		Cxt:     ctx,
		Migrate: viper.GetBool("migrate"),
	}

	return &Config{
		Fetchers: fetcher,
		Storage:  storages,
		StorageConfig: StorageConfig{
			storage.MySQL: storage.MySQLConfig{
				BaseConfig:       storageBaseConfig,
				ConnectionString: getMysqlDSN(mysqlConfig),
				TableName:        mysqlConfig["table"],
				IDGenerator:      nil,
			},
			storage.MongoDB: storage.MongoDBConfig{
				BaseConfig:       storageBaseConfig,
				ConnectionString: mongodbConfig["uri"],
				Database:         mongodbConfig["db"],
				Collection:       mongodbConfig["collection"],
			},
		},
		FetchersConfig: map[currency.Provider]interface{}{
			currency.ExchangeRatesAPIProvider: fetchers.ExchangeRatesAPIConfig{
				BaseConfig: fetchers.BaseConfig{
					Ctx: ctx,
					URL: viper.GetString("fetchers.exchangeratesapi"),
				},
			},
			currency.FreeConvProvider: fetchers.FreeConvServiceConfig{
				BaseConfig: fetchers.BaseConfig{
					Ctx: ctx,
					URL: fetcherConfig["url"],
				},
				APIKey:             fetcherConfig["apikey"],
				MaxPerHourRequests: int(maxPerHour),
				MaxPerRequest:      int(maxPerRequest),
			},
		},
		CurrenciesToFetch: viper.GetStringSlice("currencies"),
	}, nil
}
