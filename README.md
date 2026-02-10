# Firefly Bridge

### A semi-automatic bridge between banks, brokerages, and [Firefly III](https://firefly-iii.org/). 

Firefly Bridge fetches transactions and balances directly from financial institutions using browser automation and CSV exports, then imports them into [Firefly III](https://firefly-iii.org/) in a deterministic and repeatable way. The goal is not to provide a universal, plug-and-play solution, but a transparent and customizable pipeline that can be adapted to individual accounts and institutions.
Institution-specific logic—such as login flows, CSS selectors, CSV column mappings, and secret references can be defined in `config.yaml`, allowing sensitive details to remain private.