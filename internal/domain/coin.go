package domain

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

var (
	ErrRateNotFound = errors.New("rate not found")
)

type Coin struct {
	Moneda      string          `gorm:"column:Moneda;type:nvarchar(50)" json:"moneda"`
	Valor       decimal.Decimal `gorm:"column:Valor;type:money" json:"valor"`
	Fecha       time.Time       `gorm:"column:Fecha;type:datetime" json:"fecha"`
	Simbolo     string          `gorm:"column:Simbolo;type:nchar(1)" json:"simbolo"`
	FechaValida time.Time       `gorm:"column:FechaValida;type:datetime" json:"fechaValida"`
}

func (Coin) TableName() string {
	return "LOCATEL.TasasCambio"
}

type TasaCambio struct {
	Dolar Coin `gorm:"-" json:"dolar"`
	Euro  Coin `gorm:"-" json:"euro"`
	Pesos Coin `gorm:"-" json:"pesos"`
}

type HistoryFilter struct {
	Currency string
	Date     *time.Time
	From     *time.Time
	To       *time.Time
	Order    string
}

type DifferentialResponse struct {
	Currency             string          `json:"currency"`
	IssueRate            Coin            `json:"issue_rate"`
	PaymentRate          Coin            `json:"payment_rate"`
	AbsoluteDifference   decimal.Decimal `json:"absolute_difference"`
	PercentageDifference decimal.Decimal `json:"percentage_difference"`
}

type RepositoryCoin interface {
	SaveCoin(ctx context.Context, c Coin) error
	GetLatestExchangeRate(ctx context.Context) (TasaCambio, error)
	GetHistory(ctx context.Context, filter HistoryFilter) ([]Coin, error)
	GetSpecificRate(ctx context.Context, currency string, targetDate *time.Time) (Coin, error)
}
