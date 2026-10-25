package embed

import "errors"

func isUnavailable(err error) bool { return errors.Is(err, ErrUnavailable) }
