# Secret lib

Stupidly simple library to generate a random length byte slice or string.

## Example code:

```go
package main

import (
	"fmt"
	
	"github.com/nedscode/transit/lib/secret"
)

func main() {
	fmt.Println("Secret:", secret.String(64))
}
```
