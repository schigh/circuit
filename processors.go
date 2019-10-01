package circuit

import "context"

// PreProcessor is a function that runs before the main Runner function is called within Run.
// If the preprocessor returns an error, that error is returned from Run and the Runner will
// not execute.
type PreProcessor func(context.Context, Runner) (context.Context, Runner, error)

// PostProcessor is a function that runs after the main Runner function has returned a value.
type PostProcessor func(context.Context, interface{}, error) (interface{}, error)
