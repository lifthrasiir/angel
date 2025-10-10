type PrimitiveTypeMap = {
  string: string;
  'string?': string | undefined | null;
  number: number;
  boolean: boolean;
  'boolean?': boolean | undefined | null;
  object: object;
  array: any[];
  'array?': any[] | undefined | null;
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
  const expectedKeyNames = Object.keys(expectedKeys);

  // Check for unexpected keys in obj
  for (const actualKey of actualKeys) {
    if (!expectedKeyNames.includes(actualKey)) {
      return false; // Found an unexpected key
    }
  }

  for (const key in expectedKeys) {
    if (!actualKeys.includes(key)) {
      // If the key is not present, check if it's an optional type
      if (String(expectedKeys[key]).endsWith('?')) {
        continue; // Optional field not present, which is valid
      }
      return false;
    }

    const expectedType = String(expectedKeys[key]); // Convert to string to check for '?'
    const actualValue = obj[key];

    // Handle optional types
    if (expectedType.endsWith('?')) {
      const baseType = expectedType.slice(0, -1); // Remove '?'
      if (actualValue === undefined || actualValue === null) {
        continue; // Optional field is undefined or null, which is valid
      }
      // If it has a value, validate its base type
      if (baseType === 'any') {
        continue;
      } else if (baseType === 'array') {
        if (!Array.isArray(actualValue)) {
          return false;
        }
      } else if (baseType === 'object') {
        if (typeof actualValue !== 'object' || Array.isArray(actualValue) || actualValue === null) {
          return false;
        }
      } else if (typeof actualValue !== baseType) {
        return false;
      }
    } else {
      // Handle non-optional types (existing logic)
      if (expectedType === 'any') {
        continue;
      } else if (expectedType === 'array') {
        if (!Array.isArray(actualValue)) {
          return false;
        }
      } else if (expectedType === 'object') {
        if (typeof actualValue !== 'object' || Array.isArray(actualValue) || actualValue === null) {
          return false;
        }
      } else if (typeof actualValue !== expectedType) {
        return false;
      }
    }
  }
  return true; // If all checks pass, return true
}
