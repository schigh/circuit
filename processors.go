package circuit

import "context"

type PreProcessor func(context.Context, Runner) (Runner, error)

type PostProcessor func(context.Context, interface{}, error) (interface{}, error)
