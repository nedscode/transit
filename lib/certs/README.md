# Certs handling lib

This lib supports loading existing certs and generating new self-signed certificates.

## Example code:

```go
package main

import (
	"github.com/nedscode/transit/lib/certs"
	"github.com/sirupsen/logrus"
)

func main() {
	logger := logrus.New()
	
	c := certs.New(logger)
	
	// This will check if the files don't exist and if not create new files with a self-signed key/certificate pair.
	c.CheckCreateTLS("key.pem", "cert.pem", "localhost,127.0.0.1")
	
	// Loads the files' key/certificate pair
	c.LoadTLS("key.pem", "cert.pem")
	
	myCert := c.TLSCertificate()
	// ... put something that needs a `tls.Certificate` here 
}
```