package security

import "testing"

func TestPasswordHashAndBoundedParameters(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-test")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "correct-horse-battery-test") || VerifyPassword(hash, "incorrect-password") {
		t.Fatal("password verification failed")
	}
	if VerifyPassword("$argon2id$v=19$m=999999999,t=3,p=4$AAAA$AAAA", "any") {
		t.Fatal("unbounded hash parameters accepted")
	}
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
}
