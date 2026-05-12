package models

import (
	"time"

	"github.com/shopspring/decimal"
)

type Coin struct {
	ID          uint            `gorm:"primaryKey;autoIncrement"`
	Moneda      string          `gorm:"column:Moneda;type:varchar(64)" json:"moneda"`
	Valor       decimal.Decimal `gorm:"column:Valor;type:decimal(18,4)" json:"valor"`
	Fecha       time.Time       `gorm:"column:Fecha;type:datetime" json:"fecha"`
	Simbolo     string          `gorm:"column:Simbolo;type:varchar(8)" json:"simbolo"`
	FechaValida time.Time       `gorm:"column:FechaValida;type:datetime" json:"fecha_valida"`
}

func (Coin) TableName() string {
	return "TasasCambio"
}

type TasaCambio struct {
	Dolar Coin `gorm:"-" json:"dolar"`
	Euro  Coin `gorm:"-" json:"euro"`
	Pesos Coin `gorm:"-" json:"pesos"`
}
