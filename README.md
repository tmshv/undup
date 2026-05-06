# undup

Walks a directory and find all unpacked arhives to help you keep your file system clean. For example:

```
./undup ~/Downloads/GIS
Unpacked archive ne_10m_populated_places.zip (...GIS/natural_earth/ne_10m_populated_places)
Unpacked archive ne_10m_roads.zip (...GIS/natural_earth/ne_10m_roads)
Unpacked archive ne_10m_time_zones.zip (...GIS/natural_earth/ne_10m_time_zones)
```

This output shows several zip files (`ne_10m_populated_places.zip`) being unpacked inplace (`ne_10m_populated_places`).

## Usage

1. Build

```sh
go build ./cmd/undup
```

2. Run

```sh
./undup <root_dir>
```
