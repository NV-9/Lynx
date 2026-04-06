# Lynx
[Lynx](https://github.com/NV-9/Lynx) is a self-hosted URL shortener with authentication, admin user management, and a clean web UI. It is designed to run as a lightweight single-container deployment while still providing practical controls like custom slugs, filtered/paginated link browsing, and role-based actions.

## Built with
[![Go](https://img.shields.io/badge/Go-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev/)
[![Chi](https://img.shields.io/badge/Chi_Router-111111?style=for-the-badge)](https://github.com/go-chi/chi)
[![SQLite](https://img.shields.io/badge/SQLite-003B57?style=for-the-badge&logo=sqlite&logoColor=white)](https://www.sqlite.org/)
[![Docker](https://img.shields.io/badge/Docker-2496ED?style=for-the-badge&logo=docker&logoColor=white)](https://www.docker.com/)
[![Tailwind CSS](https://img.shields.io/badge/Tailwind_CSS-0F172A?style=for-the-badge&logo=tailwindcss&logoColor=38BDF8)](https://tailwindcss.com/)
[![HTMX](https://img.shields.io/badge/HTMX-1f2937?style=for-the-badge)](https://htmx.org/)

## Development
The project is production-usable and still evolving. Current work is focused on tightening admin workflows, improving UX polish, and keeping the deployment path simple for single-host setups.

## Features
- [x] Authenticated dashboard
- [x] First-user bootstrap as admin
- [x] Admin user management (create, role toggle, delete safeguards)
- [x] Custom slug support
- [x] Reserved slug protection
- [x] Link access metrics
- [x] Filtered + paginated link API
- [x] Admin-only link deletion
- [ ] Link analytics breakdown by date/user-agent

## Roadmap
- Link analytics breakdown by date range and top destinations
- CSV export for filtered link data
- Optional webhook notifications for newly created links

## Run Locally
### Run Locally - Docker
1. Clone the repository:
```sh
git clone https://github.com/NV-9/Lynx.git
cd Lynx
```
2. Start the app:
```sh
docker compose up -d
```
3. Open:
```text
http://localhost:8080
```

### Run Locally - Manual Setup
1. Clone and enter the project:
```sh
git clone https://github.com/NV-9/Lynx.git
cd Lynx
```
2. Install dependencies and run:
```sh
go mod download
go run .
```
3. Open:
```text
http://localhost:8080
```

## Environment Variables
| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP port |
| `BASE_URL` | `http://localhost:<PORT>` | Public base URL used when returning short links |
| `DATA_DIR` | `/data` | Directory used for SQLite database storage |

## API Summary
| Method | Path | Notes |
|---|---|---|
| `POST` | `/api/shorten` | Create short link (optional custom slug) |
| `GET` | `/api/links` | Auth required, supports `filter`, `page`, `size` |
| `DELETE` | `/api/links/{id}` | Admin only |
| `GET` | `/api/users` | Admin only |
| `POST` | `/api/users` | Admin only |
| `PATCH` | `/api/users/{id}/admin` | Admin only |
| `DELETE` | `/api/users/{id}` | Admin only |

## Contributing
Contributions are welcome.

- Report bugs or request features: [Issues](https://github.com/NV-9/Lynx/issues)
- Submit fixes/improvements: [Pull Requests](https://github.com/NV-9/Lynx/pulls)

1. Fork the repository.
2. Create a feature branch from `main`.
3. Add or update tests for behavior changes.
4. Run checks before opening a PR:
```sh
go test ./...
go build ./...
```
5. Open a pull request with a clear summary and rationale.

## License
This project is licensed under the MIT License.

See [LICENSE](LICENSE) for details.
