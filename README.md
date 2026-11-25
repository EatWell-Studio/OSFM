# OSFM — OpenStreetFoodMap

A food map for a circle of friends, on top of OpenStreetMap. **The map is only the display; the recommendations of people you know are the data.**

> 🚧 Research phase — there is no code yet. See [docs/research](docs/research/README.md).

一个建立在 OpenStreetMap 之上、只属于一个熟人圈的美食地图：地图只是展示层，群友署名的推荐才是数据层。

## Idea

- **One circle, one instance.** Each group of friends runs its own instance; there is no central database that mixes circles.
- **Vouched, not crowdsourced.** Joining takes an invitation, and every recommendation carries the name (or chosen pseudonym) of the member who made it — the cost of spamming the map is becoming a member of the circle.
- **Open map, own data.** OpenStreetMap as the base map; the circle's recommendations stay on the circle's own server and can be exported at any time.
- **Small enough for a set-top box.** A single process with SQLite is the target for the first version.

## Documentation

Public project documentation lives in [`docs/`](docs/README.md). The first part is the initial product research in [`docs/research/`](docs/research/README.md): product form, technology stack, open-source landscape, map data and compliance.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request. All commits must be signed off (DCO).

## Security

Please report vulnerabilities privately — see [SECURITY.md](SECURITY.md).

## License

Not chosen yet; the options are discussed in the research documents. Until a license is added, no rights are granted to use, copy or distribute the contents of this repository.

Map data © [OpenStreetMap contributors](https://www.openstreetmap.org/copyright), available under the Open Database License.
