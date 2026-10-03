# Assignment

Luckily, Polka has a solution for this: API keys. Polka provided us with an API key, and if a request to our webhook handler doesn't use that API key, we should reject the request. This ensures that only Polka can tell us to upgrade a user's account.

Your Polka key: `f271c81ff7084ee5b99a5091b42d486e`

1. Add a new secret value to your `.env` file called `POLKA_KEY`. This is the api key that polka will send so that we know it's them (and not someone else trying to get free Chirpy red). Load it into your server and store it in your `server`.

2. Add a func GetAPIKey(headers http.Header) (string, error) to your auth package. It should extract the api key from the Authorization header, which is expected to be in this format:
    ```
    Authorization: ApiKey THE_KEY_HERE
    ```
    You'll need to strip out the ApiKey part and the whitespace and return just the key.

3. Update the `POST /api/polka/webhooks` endpoint. It should ensure that the API key in the header matches the one stored in the `.env` file. If it doesn't, the endpoint should respond with a `401` status code.