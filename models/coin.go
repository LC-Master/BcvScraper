package models

import (
	"time"

	"github.com/shopspring/decimal"
)

type Coin struct {
	Moneda      string          `gorm:"column:Moneda;type:nvarchar(50)"`
	Valor       decimal.Decimal `gorm:"column:Valor;type:money"`
	Fecha       time.Time       `gorm:"column:Fecha;type:datetime"`
	Simbolo     string          `gorm:"column:Simbolo;type:nchar(1)"`
	FechaValida time.Time       `gorm:"column:FechaValida;type:datetime"`
}

func (Coin) TableName() string {
	return "dbo.[LOCATEL.TasasCambio]"
}

type TasaCambio struct {
	Dolar Coin `gorm:"-" json:"dolar"`
	Euro  Coin `gorm:"-" json:"euro"`
	Pesos Coin `gorm:"-" json:"pesos"`
}
