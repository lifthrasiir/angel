type PrimitiveTypeMap = {
  string: string;
  number: number;
  boolean: boolean;
  object: object;
  array: any[];
  any: any;
};

type AssertedType<T extends Record<string, keyof PrimitiveTypeMap>> = {
  [K in keyof T]: PrimitiveTypeMap[T[K]];
};

// Changed to a Type Guard Function (returns boolean)
export function validateExactKeys<T extends Record<string, keyof PrimitiveTypeMap>>(
  obj: any,
  expectedKeys: T,
): obj is AssertedType<T> {
  if (typeof obj !== 'object' || obj === null) {
    return false;
  }

  const actualKeys = Object.keys(obj);
  if (actualKeys.length !== Object.keys(expectedKeys).length) {
    return false;
  }

  for (const key in expectedKeys) {
    if (!actualKeys.includes(key)) {
      return false;
    }
    const expectedType = expectedKeys[key];
    const actualValue = obj[key];

    if (expectedType === 'any') {
      continue;
    } else if (expectedType === 'array') {
      if (!Array.isArray(actualValue)) {
        return false;
      }
    }
    // Check for non-null, non-array object
    else if (expectedType === 'object') {
      if (typeof actualValue !== 'object' || Array.isArray(actualValue) || actualValue === null) {
        return false;
      }
    } else if (typeof actualValue !== expectedType) {
      return false;
    }
  }
  return true; // If all checks pass, return true
}
