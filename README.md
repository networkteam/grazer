# grazer

🌱🦓

## What does it do?

Grazer implements a queued revalidation of invalidated route paths for the combination of Neos and Next.js.
Instead of figuring out the minimal set of paths (documents) to revalidate, we rather want to first revalidate the directly changed documents and revalidate all other paths after that.
Since Next.js and Neos itself are not suitable to run a queue and we have some special needs for prioritization / uniqueness, we created this small server based around a custom priority queue.
To make deployment of Neos and Next.js easier, it also does an initial revalidate of all documents after a configurable delay.

## Installation

* Deploy via Docker or run the binary
* Set flags / env vars for your specific environment
* Forward invalidate requests from Networkteam.Neos.Next to `/api/revalidate`

## Command reference

```
NAME:
   grazer - Handle invalidates from Neos CMS and revalidation in Next.js

USAGE:
   grazer [global options] command [command options] [arguments...]

COMMANDS:
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --address value                   Address for HTTP server to listen on (default: ":3100") [$GZ_ADDRESS]
   --revalidate-token value          A secret token to use for revalidation [$GZ_REVALIDATE_TOKEN]
   --next-revalidate-url value       The full URL to call to revalidate a page in Next.js [$GZ_NEXT_REVALIDATE_URL]
   --revalidate-batch-size value     The number of documents to send for revalidation in one batch to Next.js (default: 1) [$GZ_REVALIDATE_BATCH_SIZE]
   --revalidate-timeout value        Timeout for revalidation requests (default: 15s) [$GZ_REVALIDATE_TIMEOUT]
   --neos-base-url value             The base URL of the Neos CMS instance [$GZ_NEOS_BASE_URL]
   --fetch-timeout value             Timeout for fetching from the Neos content API (default: 15s) [$GZ_FETCH_TIMEOUT]
   --initial-revalidate-delay value  Delay before an initial revalidation of all pages, set to 0 to disable (default: 15s) [$GZ_INITIAL_REVALIDATE_DELAY]
   --verbose                         Enable verbose logging (default: false) [$GZ_VERBOSE]
   --help, -h                        show help
```

## Caveats

* There is no persistence yet - a restart will (cleanly) stop processing of the queue. But this should not be an issue with the full initial revalidation.

## License

[MIT](./LICENSE)
