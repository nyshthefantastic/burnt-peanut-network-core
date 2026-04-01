cmake_minimum_required(VERSION 3.22.1)
project(mljni)

set(CORE_INCLUDE "${CMAKE_SOURCE_DIR}/../../../../cabi")

add_library(core SHARED IMPORTED)
set_target_properties(core PROPERTIES
        IMPORTED_LOCATION "${CMAKE_SOURCE_DIR}/../jniLibs/${ANDROID_ABI}/libcore.so")

add_library(mljni SHARED ml_jni.cpp)
target_include_directories(mljni PRIVATE "${CORE_INCLUDE}")
target_link_libraries(mljni PRIVATE core log)
