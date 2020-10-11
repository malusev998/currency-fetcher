package services

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/shopspring/decimal"

	currency_fetcher "github.com/malusev998/currency-fetcher"
)

var (
	ErrCurrencyNotFound  = errors.New("rate for the currency is not found in storage")
	ErrNoStorageProvided = errors.New("no storage provided")
	ErrTimeRanOut        = errors.New("time has run out")
)

type ConversionService struct {
	Ctx      context.Context
	Storages []currency_fetcher.Storage
}

func (c ConversionService) Convert(from, to, provider string, value float32, date time.Time) (float32, error) {
	decimalValue := decimal.NewFromFloat32(value)
	startOfDay := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())

	convert := func(value decimal.Decimal, rate float32) (float32, error) {
		rateDecimal := decimal.NewFromFloat32(rate)
		floatValue, _ := value.Mul(rateDecimal).Float64()
		return float32(math.Round(floatValue*1_000_000) / 1_000_000), nil
	}

	if len(c.Storages) == 0 {
		return 0.0, ErrNoStorageProvided
	}

	// Optimization when there is only one storage provider

	if len(c.Storages) == 1 {
		currencies, err := c.Storages[0].GetByDateAndProvider(from, to, provider, startOfDay, date, 1, 1)

		if err != nil {
			return 0.0, err
		}

		if len(currencies) == 0 {
			return 0, ErrCurrencyNotFound
		}

		return convert(decimalValue, currencies[0].Rate)
	}

	// If there are more storage providers
	// first one that returns a value, that one will be used

	currenciesChannel := make(chan struct {
		currencies []currency_fetcher.CurrencyWithId
		error      error
	})

	defer close(currenciesChannel)

	for _, storage := range c.Storages {
		go func(storage currency_fetcher.Storage) {
			currencies, err := storage.GetByDateAndProvider(from, to, provider, startOfDay, date, 1, 1)
			currenciesChannel <- struct {
				currencies []currency_fetcher.CurrencyWithId
				error      error
			}{currencies: currencies, error: err}
		}(storage)
	}

	select {
	case <-c.Ctx.Done():
		return 0.0, ErrTimeRanOut

	case data := <-currenciesChannel:
		if data.error != nil {
			return 0.0, data.error
		}

		if len(data.currencies) == 0 {
			return 0, ErrCurrencyNotFound
		}

		return convert(decimalValue, data.currencies[0].Rate)

	}
}
