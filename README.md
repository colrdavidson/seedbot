# HLL Seedbot

Reads a JSON config file to get access info for the HLL servers it's going to manage,
and then enables disables seeding mode and late-night mode depending on
time of day and server pop

I run this through cron as a background task, once per minute
Ex:
```
* * * * * ~/seedbot/seedbot ~/seedbot/config.json >> ~/seedbot/output.log 2>&1
```

Minor caveats with the current version:
- Late-night is hardcoded to 11 PM -> 4 AM PST (converted to UTC)
- "Seeded" is 90+ people, "Unseeded" is 0 people
- There's no way to just use one map rotation set at the moment, you have to use all three
