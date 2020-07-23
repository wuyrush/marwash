# 07/18/2020

1. logging for easier debugging
2. UTs for Pinger

# 07/21/2020

1. using plain http.Client value unfortunately leads to hard-to-test code in our case; We will need to abstract it out. A good way to do this is via interface (and its decent implement-by-implicit nature), as we only use a few methods from http.Client:
```
type ReqDoer interface {
    func Do(*http.Request) (*http.Response, error)  // which *http.Client implements
}
// use a ReqDoer value instead of concrete *http.Client in our code
```
