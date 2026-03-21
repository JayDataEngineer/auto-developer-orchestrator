import { describe, it, expect, vi } from 'vitest';
import * as utils from '../../src/lib/utils';

describe('Mocks Example', () => {
  it('should demonstrate how to mock a function', () => {
    // We can spy on the cn function
    const cnSpy = vi.spyOn(utils, 'cn');

    // Call it
    utils.cn('test-class');

    // Check if it was called
    expect(cnSpy).toHaveBeenCalledWith('test-class');

    // Restore
    cnSpy.mockRestore();
  });
});
