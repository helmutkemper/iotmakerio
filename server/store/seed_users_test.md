# Create test users

```go
	cd server
	go mod tidy          # baixa o gofakeit
	go test ./store/ -run TestSeedRandomUsers -v -count=1
```

Caution, this test creates a block of 50 test users in the database
