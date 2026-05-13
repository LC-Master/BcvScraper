package models

import (
	"time"

	"github.com/shopspring/decimal"
)

type Coin struct {
	ID          uint            `gorm:"primaryKey;autoIncrement"`
	Moneda      string          `gorm:"column:Moneda;type:varchar(64)" `
	Valor       decimal.Decimal `gorm:"column:Valor;type:money" `
	Fecha       time.Time       `gorm:"column:Fecha;type:datetime" `
	Simbolo     string          `gorm:"column:Simbolo;type:varchar(8)"`
	FechaValida time.Time       `gorm:"column:FechaValida;type:datetime"`
}

func (Coin) TableName() string {
	return "TasasCambio"
}

type TasaCambio struct {
	Dolar Coin `gorm:"-" json:"dolar"`
	Euro  Coin `gorm:"-" json:"euro"`
	Pesos Coin `gorm:"-" json:"pesos"`
}
