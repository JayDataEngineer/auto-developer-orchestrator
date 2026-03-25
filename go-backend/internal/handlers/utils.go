package handlers

import (
	"math/rand"
	"time"
)

var rand = rand.New(rand.NewSource(time.Now().UnixNano()))
