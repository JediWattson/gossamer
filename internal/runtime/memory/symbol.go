package memory

type SymbolID uint64

// Symbol carries semantic identity independently from its physical Ref. A
// copied or promoted Symbol receives a new Ref but retains ID equality.
type Symbol struct {
	ID          SymbolID
	Description Value
}

func cloneSymbol(symbol Symbol) Symbol { return symbol }
