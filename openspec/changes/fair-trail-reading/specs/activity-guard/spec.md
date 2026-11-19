# Spec Delta

## ADDED Requirements

### Requirement: A backlog in one place doesn't delay the others
When the guard has more new trail files than it reads in one run, it SHALL share that run's reading among every account and region that has new files, so each reads some of its files on every run. No account or region SHALL wait for another's backlog to clear, and the order SHALL rotate so none is always read last.

#### Scenario: A busy account and an active one
- **WHEN** one account's region has 2,000 unread files and another active account writes a file
- **THEN** the other account's file is read within about a minute of landing, while the backlog is still being read

#### Scenario: An idle account bursts during a backlog
- **WHEN** an account with no files in the last 24 hours starts writing files while another account has a backlog
- **THEN** its first files are read at the next 10-minute rediscovery, and it is read every minute after that

#### Scenario: Many backlogs
- **WHEN** 40 account and region prefixes each have 100 unread files
- **THEN** each run reads about 10 files from each of them, and every file is read once within 10 runs

#### Scenario: More prefixes than the run reads
- **WHEN** more prefixes have new files than the run's file limit
- **THEN** each run reads one file from as many as the limit allows, starting after the last prefix the previous run read, so every prefix is reached within a few runs
