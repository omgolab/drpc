// Copyright 2021-2025 The Connect Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

/**
 * ContextValues is a collection of context values.
 */
export interface ContextValues {
  /**
   * get returns a context value.
   */
  get<T>(key: ContextKey<T>): T;
  /**
   * set sets a context value. It returns the ContextValues to allow chaining.
   */
  set<T>(key: ContextKey<T>, value: T): this;
  /**
   * delete deletes a context value. It returns the ContextValues to allow chaining.
   */
  delete(key: ContextKey<unknown>): this;
}

/**
 * createContextValues creates a new ContextValues.
 */
export function createContextValues(): ContextValues {
  return {
    get<T>(key: ContextKey<T>) {
      return key.id in this ? (this[key.id] as T) : key.defaultValue;
    },
    set<T>(key: ContextKey<T>, value: T) {
      this[key.id] = value;
      return this;
    },
    delete(key) {
      delete this[key.id];
      return this;
    },
  } as Record<symbol, unknown> & ContextValues;
}

/**
 * ContextKey is a unique identifier for a context value.
 */
export type ContextKey<T> = {
  id: symbol;
  defaultValue: T;
};

/**
 * createContextKey creates a new ContextKey.
 */
export function createContextKey<T>(
  defaultValue: T,
  options?: { description?: string },
): ContextKey<T> {
  return { id: Symbol(options?.description), defaultValue };
}
