# Changelog

## [1.0.0](https://github.com/infin-robotics/ulog/compare/v0.3.0...v1.0.0) (2026-10-14)


### ⚠ BREAKING CHANGES

* consumers must import github.com/infin-robotics/ulog. Public APIs and runtime behaviour are unchanged.

### Features

* read appended data sections ([14206a2](https://github.com/infin-robotics/ulog/commit/14206a2770ccb788ec445cf28ba25be379414721))
* read appended data sections ([515729d](https://github.com/infin-robotics/ulog/commit/515729d14d77a086e98a23e4809cde67f0992113))


### Code Refactoring

* migrate ulog module to infin-robotics ([fdf6cba](https://github.com/infin-robotics/ulog/commit/fdf6cba93e0bef4c1b3e1b431256a432d5fcd82c))

## [0.3.0](https://github.com/sunfish-robotics/ulog/compare/v0.2.0...v0.3.0) (2026-08-27)


### Features

* **columnar:** add configurable parquet-go output ([cd74fe8](https://github.com/sunfish-robotics/ulog/commit/cd74fe8590f4fa939f2c3829f251b33f36c8dc5b))
* **columnar:** add Parquet compression options ([d879720](https://github.com/sunfish-robotics/ulog/commit/d8797206d70e6e211267486b9947dc85942e56e1))

## [0.2.0](https://github.com/sunfish-robotics/ulog/compare/v0.1.0...v0.2.0) (2026-08-12)


### Features

* **dataset:** add CSV export ([6284307](https://github.com/sunfish-robotics/ulog/commit/62843079fd6f987796a48e022b3198ffc552be73))
* **dataset:** add CSV export ([560c80b](https://github.com/sunfish-robotics/ulog/commit/560c80b7ead1d4b1270f058b27adc24c53ab3e39))
* treat character arrays as strings ([6ce8428](https://github.com/sunfish-robotics/ulog/commit/6ce842851d6f9dc05684b05029a5e63b806f1a5c))
* treat character arrays as strings ([9d9d9c0](https://github.com/sunfish-robotics/ulog/commit/9d9d9c0229784cd10328b5e92306dc5d0bdb12d7))


### Bug Fixes

* preserve multi-information groups ([7b33437](https://github.com/sunfish-robotics/ulog/commit/7b33437e01a3818fd89b432e92191e8cf01f7bb7))
* preserve multi-information groups ([e6f2071](https://github.com/sunfish-robotics/ulog/commit/e6f207138854b6626bab8af0c46e48a3e2627081))

## 0.1.0 (2026-08-12)


### Features

* add column-oriented datasets ([a42acf7](https://github.com/sunfish-robotics/ulog/commit/a42acf7da862e8d0f0f3a36e7f89e10efb28a789))
* add dynamic ULog format model ([7d0ebd3](https://github.com/sunfish-robotics/ulog/commit/7d0ebd3bb4313b94a773fc04d16d7a56ae20b253))
* add dynamic ULog record reader ([59b7629](https://github.com/sunfish-robotics/ulog/commit/59b7629924bed2c3fab96549d03d7f56bf5aeb12))
* add typed and dynamic ULog writer ([10f223b](https://github.com/sunfish-robotics/ulog/commit/10f223ba2a158df5f3d8a1541e393cc815e20047))
* add typed format adapters ([1393307](https://github.com/sunfish-robotics/ulog/commit/1393307cefd3d571cb14e73481df74814ab94df8))
* **columnar:** add Arrow and Parquet export ([fb60709](https://github.com/sunfish-robotics/ulog/commit/fb6070916c40adf51fab85432a4fa24197599c3a))
* preserve ULog metadata and events ([b46bdb1](https://github.com/sunfish-robotics/ulog/commit/b46bdb15c7cf32b6b794248eb28ab6bb055e4471))
* **wire:** add ULog message payload codecs ([2d05ae1](https://github.com/sunfish-robotics/ulog/commit/2d05ae1d61545362d846b80af33912d517f2959d))
* **wire:** define ULog wire types ([324a2f1](https://github.com/sunfish-robotics/ulog/commit/324a2f1ffa13591b92eda62d9017a655c5d55f2e))


### Bug Fixes

* bound dynamic layouts to ULog message size ([3cb7d84](https://github.com/sunfish-robotics/ulog/commit/3cb7d848bff539a69ee3f7a65b4ad347a92ce81c))
* enforce ULog schema and metadata rules ([6a2f554](https://github.com/sunfish-robotics/ulog/commit/6a2f554d55027a59949e5ef01e0a61db243c3f0f))
* keep interoperability helpers test-only ([eb91be3](https://github.com/sunfish-robotics/ulog/commit/eb91be3ac7ceacc61bc835bf155869084e805663))
