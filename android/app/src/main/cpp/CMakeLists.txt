cmake_minimum_required(VERSION 3.22.1)
project(mljni)

# Resolve paths relative to this CMakeLists location.
# app/src/main/cpp -> repo root is ../../../../../
set(CORE_INCLUDE "${CMAKE_CURRENT_LIST_DIR}/../../../../../cabi")
set(CORE_LIB_DIR "${CMAKE_CURRENT_LIST_DIR}/../jniLibs/${ANDROID_ABI}")

add_library(mljni SHARED ml_jni.cpp)
target_include_directories(mljni PRIVATE "${CORE_INCLUDE}")
target_link_directories(mljni PRIVATE "${CORE_LIB_DIR}")
target_link_options(mljni PRIVATE
        "-Wl,-z,max-page-size=16384"
        "-Wl,-z,common-page-size=16384")
target_link_libraries(mljni PRIVATE core log)
