import Ajv from 'ajv';
import addFormats from 'ajv-formats';
import { expect } from '@playwright/test';
import contract from '../contracts/openapi.json';

const ajv = new Ajv({ strict: false, allErrors: true });
addFormats(ajv);
const schema = { $id: 'swimming-contract', components: contract.components };
ajv.addSchema(schema);

export function expectContract(name: keyof typeof contract.components.schemas, data: unknown) {
  const validate = ajv.compile({ $ref: `swimming-contract#/components/schemas/${name}` });
  expect(validate(data), JSON.stringify(validate.errors)).toBe(true);
}
