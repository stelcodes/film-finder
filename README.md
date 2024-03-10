# Film Finder

A web scraping program that finds classic movie screenings and prints them out.

## Bugs
- Hollywood theater non-zero minute number becomes zero somehow (7:30 -> 7:00)

## TODO

## Benchmarks

### Parallelizing Requests
Before:
```

________________________________________________________
Executed in   26.99 secs      fish           external
   usr time  525.83 millis  281.00 micros  525.55 millis
   sys time  259.26 millis  166.00 micros  259.09 millis

```
After:
```
________________________________________________________
Executed in   10.60 secs      fish           external
   usr time  565.64 millis    0.00 micros  565.64 millis
   sys time  260.37 millis  468.00 micros  259.90 millis
```
