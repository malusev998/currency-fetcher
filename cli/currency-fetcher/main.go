package main

import (
	"context"
	"database/sql"
	"github.com/BrosSquad/currency-fetcher/cli/cmd"
	"log"
	"os"
	"os/signal"
	"strconv"

	"github.com/BrosSquad/currency-fetcher"
	"github.com/BrosSquad/currency-fetcher/currency"
	"github.com/BrosSquad/currency-fetcher/services"
	"github.com/BrosSquad/currency-fetcher/storage"
	"github.com/go-sql-driver/mysql"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Config struct {
	Storages              []currency_fetcher.Storage
	FreeConvServiceConfig struct {
		ApiKey             string
		MaxPerHourRequests int
		MaxPerRequest      int
	}
	CurrenciesToFetch []string
}

func getMysqlDSN(config map[string]string) string {
	mysqlDriverConfig := mysql.NewConfig()
	mysqlDriverConfig.User = config["user"]
	mysqlDriverConfig.Passwd = config["password"]
	mysqlDriverConfig.Addr = config["addr"]
	mysqlDriverConfig.DBName = config["db"]
	return mysqlDriverConfig.FormatDSN()
}

func getConfig(ctx context.Context, viperConfig *viper.Viper, sqlDb **sql.DB, mongoDbClient **mongo.Client) Config {
	var config Config
	var err error
	viperConfig.SetConfigName("config")
	viperConfig.SetConfigType("yaml")
	viperConfig.AddConfigPath(".")

	config.Storages = make([]currency_fetcher.Storage, 0, 2)

	if err := viperConfig.ReadInConfig(); err != nil {
		log.Fatalf("Error while reading in the config file: %v", err)
	}

	migrate := viperConfig.GetBool("migrate")

	for _, st := range viperConfig.GetStringSlice("storage") {
		switch st {
		case "mysql":
			mysqlConfig := viperConfig.GetStringMapString("databases.mysql")

			*sqlDb, err = sql.Open("mysql", getMysqlDSN(mysqlConfig))
			if err != nil {
				log.Fatalf("Error while connecting to mysql: %v", err)
			}

			mysqlStorage := storage.NewMySQLStorage(ctx, *sqlDb, mysqlConfig["table"], nil)

			if migrate {
				if err := mysqlStorage.Migrate(); err != nil {
					log.Fatalf("Error while migrating mysql database: %v", err)
				}
			}

			config.Storages = append(config.Storages, mysqlStorage)
		case "mongodb":
			mongodbConfig := viperConfig.GetStringMapString("databases.mongo")
			*mongoDbClient, err = mongo.NewClient(options.Client().ApplyURI(mongodbConfig["uri"]))

			if err != nil {
				log.Fatalf("Error in mongo mongodbConfiguration: %v", err)
			}

			if err := (*mongoDbClient).Connect(ctx); err != nil {
				log.Fatalf("Error while connecting to mongodb: %v", err)
			}
			db := (*mongoDbClient).Database(mongodbConfig["db"])

			mongoStorage := storage.NewMongoStorage(ctx, db.Collection(mongodbConfig["collection"]))

			if migrate {
				if err := db.CreateCollection(ctx, mongodbConfig["collection"]); err != nil {
					if _, ok := err.(mongo.CommandError); !ok {
						log.Fatalf("Error while creating mongodb collection: %v", err)
					}
				}

				if err := mongoStorage.Migrate(); err != nil {
					log.Fatalf("Error while migrating mongodb collection: %v", err)
				}
			}

			config.Storages = append(config.Storages, mongoStorage)
		}
	}

	fetcherConfig := viperConfig.GetStringMapString("fetchers.freecurrconv")
	maxPerHour, err := strconv.ParseUint(fetcherConfig["maxperhour"], 10, 32)

	if err != nil {
		log.Fatalf("Error while parsing maxPerHour in fetchers.freecurrconv: %v", err)
	}

	config.FreeConvServiceConfig.MaxPerHourRequests = int(maxPerHour)

	maxPerRequest, err := strconv.ParseUint(fetcherConfig["maxperrequest"], 10, 32)
	if err != nil {
		log.Fatalf("Error while parsing maxPerRequest in fetchers.freecurrconv: %v", err)
	}

	config.FreeConvServiceConfig.MaxPerRequest = int(maxPerRequest)
	config.FreeConvServiceConfig.ApiKey = fetcherConfig["apikey"]
	config.CurrenciesToFetch = viperConfig.GetStringSlice("currencies")
	return config
}

func main() {
	var mongoDbClient *mongo.Client
	var sqlDb *sql.DB
	storages := make([]currency_fetcher.Storage, 0, 2)
	ctx, cancel := context.WithCancel(context.Background())
	signalChannel := make(chan os.Signal, 1)
	config := getConfig(ctx, viper.New(), &sqlDb, &mongoDbClient)

	service := services.FreeConvService{
		Fetcher: currency.FreeCurrConvFetcher{
			ApiKey:        config.FreeConvServiceConfig.ApiKey,
			MaxPerHour:    config.FreeConvServiceConfig.MaxPerHourRequests,
			MaxPerRequest: config.FreeConvServiceConfig.MaxPerRequest,
		},
		Storage: storages,
	}

	signal.Notify(signalChannel, os.Interrupt, os.Kill)

	go func(signalChannel <-chan os.Signal, cancel context.CancelFunc) {
		select {
		case <-signalChannel:
			cancel()
		}
	}(signalChannel, cancel)

	err := cmd.Execute(&cmd.Config{
		Ctx:               ctx,
		CurrenciesToFetch: config.CurrenciesToFetch,
		CurrencyService:   service,
	})

	if err != nil {
		log.Fatalf("Error while executing command: %v", err)
	}

	if mongoDbClient != nil {
		if err := mongoDbClient.Disconnect(ctx); err != nil {
			log.Printf("Disconnecting from mongodb failed: %v", err)
		}
	}

	if sqlDb != nil {
		if err := sqlDb.Close(); err != nil {
			log.Printf("Disconnecting from SQL Database failed: %v", err)
		}
	}
}
