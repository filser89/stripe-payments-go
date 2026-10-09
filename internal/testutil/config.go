package testutil

func Environment(databaseURL, listenAddress string) map[string]string {
	return map[string]string{"DATABASE_URL": databaseURL, "LISTEN_ADDR": listenAddress, "BASIC_AUTH_USERNAME": "checkout-fixture-user", "BASIC_AUTH_PASSWORD": "checkout-fixture-password", "STRIPE_SECRET_KEY": "sk_test_fixture", "APP_BASE_URL": "http://localhost:8080"}
}
