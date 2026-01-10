#ifndef PYTHON_WRAPPER_H
#define PYTHON_WRAPPER_H

#include <stdint.h>
#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef intptr_t Py_ssize_t;
typedef void* (*lookup_func_callback)(const char* func_name);

int store_lookup_callback(void* callback);

void* lookup_func(lookup_func_callback callback, const char* func_name);

extern void (*pythonHandle)(void);
extern void (*PyGILState_Ensure)(void);
extern void (*PyGILState_Release)(void*);
extern void* (*PyDict_New)(void);
extern void* (*PyRun_String)(const char*, int, void*, void*);
extern void* (*PyErr_Occurred)(void);
extern void (*PyErr_Clear)(void);
extern void (*Py_DecRef)(void*);
extern void (*PyErr_Print)(void);
extern void* (*PyDict_GetItemString)(void*, const char*);
extern void* (*PyObject_CallObject)(void*, void*);
extern long (*PyLong_AsLong)(void*);
extern double (*PyFloat_AsDouble)(void*);
extern int (*PyObject_IsTrue)(void*);
extern const char* (*PyUnicode_AsUTF8)(void*, Py_ssize_t*);
extern void* (*PyObject_Type)(void*);
extern void* (*PyObject_Str)(void* obj);
extern void* PyBool_Type;

// Wrapper functions
void* C_PyDict_New(void);
void* C_PyRun_String(const char* str, int start, void* globals, void* locals);
void* C_PyErr_Occurred(void);
void C_PyErr_Clear(void);
void C_Py_DecRef(void* obj);
void C_PyErr_Print(void);
void* C_PyDict_GetItemString(void* p, const char* key);
void* C_PyObject_CallObject(void* callable, void* args);
long C_PyLong_AsLong(void* obj);
double C_PyFloat_AsDouble(void* obj);
int C_PyObject_IsTrue(void* obj);
const char* C_PyUnicode_AsUTF8(void* obj);
void* C_PyObject_Type(void* obj);
int C_IsBool(void* obj);
int C_IsLong(void* obj);
int C_IsFloat(void* obj);
int C_IsUtf8String(void* obj);
const char* C_PyType_GetName(void* obj);
void* C_PyObject_Str(void* obj);

#ifdef __cplusplus
}
#endif

#endif // PYTHON_WRAPPER_H
