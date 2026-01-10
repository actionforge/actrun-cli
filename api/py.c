#include <stdio.h>
#include "py.h"

static lookup_func_callback lookup_func_cb = NULL;

typedef struct {
    const char* func_name;
    void** func_ptr;
} FuncEntry;

void (*PyGILState_Ensure)(void) = NULL;
void (*PyGILState_Release)(void*) = NULL;
void* (*PyDict_New)(void) = NULL;
void* (*PyRun_String)(const char*, int, void*, void*) = NULL;
void* (*PyErr_Occurred)(void) = NULL;
void (*PyErr_Clear)(void) = NULL;
void (*Py_DecRef)(void*) = NULL;
void (*PyErr_Print)(void) = NULL;
void* (*PyDict_GetItemString)(void*, const char*) = NULL;
void* (*PyObject_CallObject)(void*, void*) = NULL;
long (*PyLong_AsLong)(void*) = NULL;
double (*PyFloat_AsDouble)(void*) = NULL;
int (*PyObject_IsTrue)(void*) = NULL;
const char* (*PyUnicode_AsUTF8)(void*, Py_ssize_t*) = NULL;
void* (*PyObject_Type)(void*) = NULL;
void* (*PyObject_Str)(void*) = NULL;
void* PyBool_Type = NULL;
void* PyLong_Type = NULL;
void* PyFloat_Type = NULL;
void* PyUnicode_Type = NULL;

int store_lookup_callback(void* callback) {
    lookup_func_cb = callback;

    FuncEntry entries[] = {
        // functions
        {"PyGILState_Ensure", (void*)&PyGILState_Ensure},
        {"PyGILState_Release", (void*)&PyGILState_Release},
        {"PyDict_New", (void*)&PyDict_New},
        {"PyRun_String", (void*)&PyRun_String},
        {"PyErr_Occurred", (void*)&PyErr_Occurred},
        {"PyErr_Clear", (void*)&PyErr_Clear},
        {"Py_DecRef", (void*)&Py_DecRef},
        {"PyErr_Print", (void*)&PyErr_Print},
        {"PyDict_GetItemString", (void*)&PyDict_GetItemString},
        {"PyObject_CallObject", (void*)&PyObject_CallObject},
        {"PyLong_AsLong", (void*)&PyLong_AsLong},
        {"PyFloat_AsDouble", (void*)&PyFloat_AsDouble},
        {"PyObject_IsTrue", (void*)&PyObject_IsTrue},
        {"PyUnicode_AsUTF8", (void*)&PyUnicode_AsUTF8},
        {"PyObject_Str", (void*)&PyObject_Str},
        // types
        {"PyObject_Type", (void*)&PyObject_Type},
        {"PyFloat_Type", (void*)&PyFloat_Type},
        {"PyUnicode_Type", (void*)&PyUnicode_Type},
        {"PyBool_Type", (void*)&PyBool_Type},
        {"PyLong_Type", (void*)&PyLong_Type},
    };

    int num_entries = sizeof(entries) / sizeof(entries[0]);
    for (int i = 0; i < num_entries; ++i) {
        *entries[i].func_ptr = (void*)lookup_func_cb(entries[i].func_name);;
        if (*entries[i].func_ptr == NULL) {
            printf("actrun: failed to load '%s'\n", entries[i].func_name);
            return -1;
        }
    }

    return 0;
}

void* C_PyDict_New(void) {
    return PyDict_New();
}

void* C_PyRun_String(const char* str, int start, void* globals, void* locals) {
    return PyRun_String(str, start, globals, locals);
}

void* C_PyErr_Occurred(void) {
    return PyErr_Occurred();
}

void C_PyErr_Clear(void) {
    PyErr_Clear();
}

void C_Py_DecRef(void* obj) {
    Py_DecRef(obj);
}

void C_PyErr_Print(void) {
    PyErr_Print();
}

void* C_PyDict_GetItemString(void* p, const char* key) {
    return PyDict_GetItemString(p, key);
}

void* C_PyObject_CallObject(void* callable, void* args) {
    return PyObject_CallObject(callable, args);
}

void* C_PyObject_Str(void* obj) {
    return PyObject_Str(obj);
}

long C_PyLong_AsLong(void* obj) {
    return PyLong_AsLong(obj);
}

double C_PyFloat_AsDouble(void* obj) {
    return PyFloat_AsDouble(obj);
}

int C_PyObject_IsTrue(void* obj) {
    return PyObject_IsTrue(obj);
}

const char* C_PyUnicode_AsUTF8(void* obj) {
    return PyUnicode_AsUTF8(obj, NULL);
}

void* C_PyObject_Type(void* obj) {
    return PyObject_Type(obj);
}

int C_IsBool(void* obj) {
    void* type = C_PyObject_Type(obj);
    if (type == PyBool_Type) {
        C_Py_DecRef(type);
        return 1; // is a boolean
    }
    C_Py_DecRef(type);
    return 0; // not a boolean
}

int C_IsLong(void* obj) {
    void* type = C_PyObject_Type(obj);
    if (type == PyLong_Type) {
        C_Py_DecRef(type);
        return 1; // is a boolean
    }
    C_Py_DecRef(type);
    return 0; // not a boolean
}

int C_IsFloat(void* obj) {
    void* type = C_PyObject_Type(obj);
    if (type == PyFloat_Type) {
        C_Py_DecRef(type);
        return 1; // is a boolean
    }
    C_Py_DecRef(type);
    return 0; // not a boolean
}

int C_IsUtf8String(void* obj) {
    void* type = C_PyObject_Type(obj);
    if (type == PyUnicode_Type) {
        C_Py_DecRef(type);
        return 1; // is a boolean
    }
    C_Py_DecRef(type);
    return 0; // not a boolean
}

const char* C_PyType_GetName(void* obj) {
    void* type = C_PyObject_Type(obj);
    if (type != NULL) {
        void* typeStr = C_PyObject_Str(type);
        if (typeStr != NULL) {
            // potential mem leak since name is not cleaned up?
            const char* name = C_PyUnicode_AsUTF8(typeStr);
            C_Py_DecRef(typeStr);
            C_Py_DecRef(type);
            return name; // is a boolean
        }
        C_Py_DecRef(type);
    }
    C_Py_DecRef(type);
    return "unknown";
}