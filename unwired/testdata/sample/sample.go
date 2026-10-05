// Package sample is the fixture for unwired.Analyze. Every declaration here is
// labelled with what the scanner must say about it, so a wrong answer is a
// failing test rather than a judgement call.
package sample

// ReadField is read. reads=1
// WrittenOnlyField is assigned and never consulted. reads=0 writes=1
// NeverTouchedField is neither. reads=0 writes=0
// AccumulatedField is only ever `+=`, which consults the old value. reads>=1
type Config struct {
	ReadField         string
	WrittenOnlyField  int
	NeverTouchedField bool
	AccumulatedField  int
}

// UsedType is named by a parameter. reads>=1
type UsedType struct{ N int }

// UnusedType is named nowhere. reads=0
type UnusedType struct{ N int }

// UsedConst is read. reads=1
const UsedConst = "used"

// UnusedConst is read nowhere. reads=0
const UnusedConst = "unused"

// UsedVar is read. reads>=1
var UsedVar = 1

// UnusedVar is read nowhere. reads=0
var UnusedVar = 2

// Build populates a Config. It WRITES WrittenOnlyField and never reads it,
// which is the class whole-program reachability cannot see.
func Build() Config {
	c := Config{
		ReadField: UsedConst,
	}
	c.WrittenOnlyField = UsedVar
	c.AccumulatedField += 1
	return c
}

// Consume reads ReadField and AccumulatedField.
func Consume(c Config, u UsedType) string {
	_ = u.N
	_ = c.AccumulatedField
	return c.ReadField
}

// UsedFunc is called by Entry. reads=1
func UsedFunc() {}

// UnusedFunc is called nowhere. reads=0
func UnusedFunc() {}

// Method on UsedType, called by Entry. reads=1
func (u UsedType) Method() int { return u.N }

// UnusedMethod is called nowhere. reads=0
func (u UsedType) UnusedMethod() int { return u.N }

// Entry wires the live things together.
func Entry() {
	UsedFunc()
	c := Build()
	_ = Consume(c, UsedType{N: 1})
	_ = UsedType{}.Method()
}

// OnlyTestUsesThis is called only from sample_test.go. With the default
// settings that is reads=0; with TestsAsReads it is reads=1.
func OnlyTestUsesThis() int { return 1 }

// Handler is an interface with one method production code CALLS and one it does
// not. It is the shape every parser, plugin and handler in the estate has.
type Handler interface {
	// Handle is called through the interface by Dispatch. reads>=1
	Handle() int
	// Describe is called nowhere, by anyone. reads=0
	Describe() string
}

// Impl satisfies Handler.
type Impl struct{}

// Handle is reached only through Handler, never by name. It must NOT be
// reported: the call site names the interface method.
func (Impl) Handle() int { return 1 }

// Describe is reached by nothing, through the interface or otherwise, so it
// must still be reported. An interface method nobody calls does not make its
// implementations live.
func (Impl) Describe() string { return "" }

// Dispatch calls Handle through the interface.
func Dispatch(h Handler) int { return h.Handle() }

// WireHandler keeps Impl and Dispatch reachable.
func WireHandler() int { return Dispatch(Impl{}) }

// KeyConst is read as a map key in a composite literal. reads>=1, writes=0
const KeyConst = "key"

// KeyVar is read as a map key in a composite literal. reads>=1, writes=0
var KeyVar = "other"

// IndexConst is read as an array index in a composite literal. reads>=1, writes=0
const IndexConst = 1

// AssignedOnlyVar is assigned and read nowhere. reads=0, writes>=1
var AssignedOnlyVar int

// WireLiteralKeys uses each key above once.
func WireLiteralKeys() (map[string]int, [2]int) {
	AssignedOnlyVar = 7
	return map[string]int{KeyConst: 1, KeyVar: 2}, [2]int{IndexConst: 3}
}
