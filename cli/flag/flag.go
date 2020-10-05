package flag

import (
	"time"

	charts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/spf13/pflag"
)

type (
	// Flag is an interface which describes a command line flag that affects the
	// Helm Values used to render Helm charts.  This interface allows us to
	// iterate over flags which have been set and apply their effects to the
	// Values.
	Flag interface {
		Apply(values *charts.Values) error
		IsSet() bool
		Name() string
	}

	// UintFlag is a Flag with a uint typed value.
	UintFlag struct {
		name    string
		Value   uint
		flagSet *pflag.FlagSet
		apply   func(values *charts.Values, value uint) error
	}

	// Int64Flag is a Flag with an int64 typed value.
	Int64Flag struct {
		name    string
		Value   int64
		flagSet *pflag.FlagSet
		apply   func(values *charts.Values, value int64) error
	}

	// StringFlag is a Flag with a string typed value.
	StringFlag struct {
		name    string
		Value   string
		flagSet *pflag.FlagSet
		apply   func(values *charts.Values, value string) error
	}

	// StringSliceFlag is a Flag with a []string typed value.
	StringSliceFlag struct {
		name    string
		Value   []string
		flagSet *pflag.FlagSet
		apply   func(values *charts.Values, value []string) error
	}

	// BoolFlag is a Flag with a bool typed value.
	BoolFlag struct {
		name    string
		Value   bool
		flagSet *pflag.FlagSet
		apply   func(values *charts.Values, value bool) error
	}

	// DurationFlag is a Flag with a time.Duration typed value.
	DurationFlag struct {
		name    string
		Value   time.Duration
		flagSet *pflag.FlagSet
		apply   func(values *charts.Values, value time.Duration) error
	}
)

// NewUintFlag creates a new uint typed Flag that executes the given function
// when applied.  The flag is attached to the given FlagSet.
func NewUintFlag(flagSet *pflag.FlagSet, name string, defaultValue uint, description string, apply func(values *charts.Values, value uint) error) *UintFlag {
	flag := UintFlag{
		name:    name,
		flagSet: flagSet,
		apply:   apply,
	}
	flagSet.UintVar(&flag.Value, name, defaultValue, description)
	return &flag
}

// NewInt64Flag creates a new int64 typed Flag that executes the given function
// when applied.  The flag is attached to the given FlagSet.
func NewInt64Flag(flagSet *pflag.FlagSet, name string, defaultValue int64, description string, apply func(values *charts.Values, value int64) error) *Int64Flag {
	flag := Int64Flag{
		name:    name,
		flagSet: flagSet,
		apply:   apply,
	}
	flagSet.Int64Var(&flag.Value, name, defaultValue, description)
	return &flag
}

// NewStringFlag creates a new string typed Flag that executes the given function
// when applied.  The flag is attached to the given FlagSet.
func NewStringFlag(flagSet *pflag.FlagSet, name string, defaultValue string, description string, apply func(values *charts.Values, value string) error) *StringFlag {
	flag := StringFlag{
		name:    name,
		flagSet: flagSet,
		apply:   apply,
	}
	flagSet.StringVar(&flag.Value, name, defaultValue, description)
	return &flag
}

// NewStringSliceFlag creates a new []string typed Flag that executes the given function
// when applied.  The flag is attached to the given FlagSet.
func NewStringSliceFlag(flagSet *pflag.FlagSet, name string, defaultValue []string, description string, apply func(values *charts.Values, value []string) error) *StringSliceFlag {
	flag := StringSliceFlag{
		name:    name,
		flagSet: flagSet,
		apply:   apply,
	}
	flagSet.StringSliceVar(&flag.Value, name, defaultValue, description)
	return &flag
}

// NewStringFlagP creates a new string typed Flag that executes the given function
// when applied.  The flag is attached to the given FlagSet.
func NewStringFlagP(flagSet *pflag.FlagSet, name string, short string, defaultValue string, description string, apply func(values *charts.Values, value string) error) *StringFlag {
	flag := StringFlag{
		name:    name,
		flagSet: flagSet,
		apply:   apply,
	}
	flagSet.StringVarP(&flag.Value, name, short, defaultValue, description)
	return &flag
}

// NewBoolFlag creates a new bool typed Flag that executes the given function
// when applied.  The flag is attached to the given FlagSet.
func NewBoolFlag(flagSet *pflag.FlagSet, name string, defaultValue bool, description string, apply func(values *charts.Values, value bool) error) *BoolFlag {
	flag := BoolFlag{
		name:    name,
		flagSet: flagSet,
		apply:   apply,
	}
	flagSet.BoolVar(&flag.Value, name, defaultValue, description)
	return &flag
}

// NewDurationFlag creates a new time.Duration typed Flag that executes the given function
// when applied.  The flag is attached to the given FlagSet.
func NewDurationFlag(flagSet *pflag.FlagSet, name string, defaultValue time.Duration, description string, apply func(values *charts.Values, value time.Duration) error) *DurationFlag {
	flag := DurationFlag{
		name:    name,
		flagSet: flagSet,
		apply:   apply,
	}
	flagSet.DurationVar(&flag.Value, name, defaultValue, description)
	return &flag
}

// Apply executes the stored apply function on the given Values.
func (flag *UintFlag) Apply(values *charts.Values) error {
	return flag.apply(values, flag.Value)
}

// IsSet returns true if and only if the Flag has been explicitly set with a value.
func (flag *UintFlag) IsSet() bool {
	return flag.flagSet.Changed(flag.name)
}

// Name returns the name of the flag.
func (flag *UintFlag) Name() string {
	return flag.name
}

// Apply executes the stored apply function on the given Values.
func (flag *Int64Flag) Apply(values *charts.Values) error {
	return flag.apply(values, flag.Value)
}

// IsSet returns true if and only if the Flag has been explicitly set with a value.
func (flag *Int64Flag) IsSet() bool {
	return flag.flagSet.Changed(flag.name)
}

// Name returns the name of the flag.
func (flag *Int64Flag) Name() string {
	return flag.name
}

// Apply executes the stored apply function on the given Values.
func (flag *StringFlag) Apply(values *charts.Values) error {
	return flag.apply(values, flag.Value)
}

// IsSet returns true if and only if the Flag has been explicitly set with a value.
func (flag *StringFlag) IsSet() bool {
	return flag.flagSet.Changed(flag.name)
}

// Name returns the name of the flag.
func (flag *StringFlag) Name() string {
	return flag.name
}

// Apply executes the stored apply function on the given Values.
func (flag *StringSliceFlag) Apply(values *charts.Values) error {
	return flag.apply(values, flag.Value)
}

// IsSet returns true if and only if the Flag has been explicitly set with a value.
func (flag *StringSliceFlag) IsSet() bool {
	return flag.flagSet.Changed(flag.name)
}

// Name returns the name of the flag.
func (flag *StringSliceFlag) Name() string {
	return flag.name
}

// Apply executes the stored apply function on the given Values.
func (flag *BoolFlag) Apply(values *charts.Values) error {
	return flag.apply(values, flag.Value)
}

// IsSet returns true if and only if the Flag has been explicitly set with a value.
func (flag *BoolFlag) IsSet() bool {
	return flag.flagSet.Changed(flag.name)
}

// Name returns the name of the flag.
func (flag *BoolFlag) Name() string {
	return flag.name
}

// Apply executes the stored apply function on the given Values.
func (flag *DurationFlag) Apply(values *charts.Values) error {
	return flag.apply(values, flag.Value)
}

// IsSet returns true if and only if the Flag has been explicitly set with a value.
func (flag *DurationFlag) IsSet() bool {
	return flag.flagSet.Changed(flag.name)
}

// Name returns the name of the flag.
func (flag *DurationFlag) Name() string {
	return flag.name
}

// ApplySetFlags iterates through the given slice of Flags and applies the
// effect of each set flag to the given Values.  Flags effects are applied
// in the order they appear in the slice.
func ApplySetFlags(values *charts.Values, flags []Flag) error {
	for _, flag := range flags {
		if flag.IsSet() {
			err := flag.Apply(values)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
