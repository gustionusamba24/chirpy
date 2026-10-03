# Assignment

We recently rolled out a new feature called "Chirpy Red". It's a membership program, and members of "Chirpy Red" get pretty incredible features: like the ability to edit chirps after posting them. But that's beside the point...

Chirpy uses "Polka" as its payment provider. They send us webhooks whenever a user subscribes to Chirpy Red. We need to mark users as Chirpy Red members when we receive these webhooks.

1. Add a migration to the users table to include a new column called is_chirpy_red. This column should be a boolean, and it should default to false.

2. Add a database query that upgrades a user to chirpy red based on their ID.

3. Add a POST /api/polka/webhooks endpoint. It should accept a request of this shape:
    ```json
    {
        "event": "user.upgraded",
        "data": {
            "user_id": "3311741c-680c-4546-99f3-fc9efac2036c"
        }
    }
    ```
    - If the `event` is anything other than `user.upgraded`, the endpoint should immediately respond with a `204` status code - we don't care about any other events.
    - If the `event` is `user.upgraded`, then it should update the user in the database, and mark that they are a Chirpy Red member.
    - If the user is upgraded successfully, the endpoint should respond with a `204` status code and an empty response body. If the user can't be found, the endpoint should respond with a `404` status code.

    _Polka uses the response code to know whether or not the webhook was received successfully. If the response code is anything other than `2XX`, they'll retry the request_.

4. Update all endpoints that return user resources to include the `is_chirpy_red` field.