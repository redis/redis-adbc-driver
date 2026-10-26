cmake_minimum_required(VERSION 3.15...3.29)
if(POLICY CMP0135)
  cmake_policy(SET CMP0135 NEW)
endif()
project(redis_adbc LANGUAGES C CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
if(WIN32 OR EMSCRIPTEN)
  message(FATAL_ERROR "redis_adbc currently supports native Linux and macOS builds")
endif()
include(duckdb-extension/go-toolchain.cmake)
find_package(Threads REQUIRED)

# The driver and Go runtime are embedded in the extension: no driver manager,
# manifest, shared library, or adbc_scanner installation is required at runtime.
file(GLOB_RECURSE REDIS_GO_SOURCES CONFIGURE_DEPENDS
     "${CMAKE_CURRENT_SOURCE_DIR}/go/*.go"
     "${CMAKE_CURRENT_SOURCE_DIR}/go/pkg/*.c"
     "${CMAKE_CURRENT_SOURCE_DIR}/go/pkg/*.h")
set(REDIS_GO_ARCHIVE "${CMAKE_CURRENT_BINARY_DIR}/libredis_adbc_driver.a")
add_custom_command(
  OUTPUT "${REDIS_GO_ARCHIVE}"
  COMMAND "${CMAKE_COMMAND}" -E env CGO_ENABLED=1
          "GOARCH=${REDIS_GO_ARCH}" "GOTOOLCHAIN=local"
          "MACOSX_DEPLOYMENT_TARGET=12.0"
          "CC=${CMAKE_C_COMPILER}" "CXX=${CMAKE_CXX_COMPILER}"
          "${REDIS_GO_EXECUTABLE}" build -buildmode=c-archive -tags driverlib,duckdb
          -trimpath -o "${REDIS_GO_ARCHIVE}" ./pkg
  WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}/go"
  DEPENDS ${REDIS_GO_SOURCES} go/go.mod go/go.sum
  COMMENT "Building the embedded Redis ADBC driver and Go runtime")
add_custom_target(redis_adbc_driver DEPENDS "${REDIS_GO_ARCHIVE}")

include_directories(duckdb-extension/include go/pkg)
build_static_extension(redis_adbc duckdb-extension/redis_adbc_extension.cpp)
build_loadable_extension(redis_adbc "" duckdb-extension/redis_adbc_extension.cpp)
foreach(target redis_adbc_extension redis_adbc_loadable_extension)
  if(COMMAND duckdb_extension_statically_link)
    target_compile_definitions(${target} PRIVATE REDIS_ADBC_DUCKDB_V2)
  endif()
  add_dependencies(${target} redis_adbc_driver)
  target_link_libraries(${target} "${REDIS_GO_ARCHIVE}" Threads::Threads ${CMAKE_DL_LIBS})
  if(APPLE)
    target_compile_options(${target} PRIVATE -mmacosx-version-min=12.0)
    target_link_options(${target} PRIVATE -mmacosx-version-min=12.0)
    target_link_libraries(${target} "-framework CoreFoundation" "-framework Security" resolv)
  endif()
endforeach()
