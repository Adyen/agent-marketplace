export interface GenerateClientOptions {
  inputPath: string
  outputPath: string
  check: boolean
  openapiTS: (input: any, options?: any) => Promise<any>
  astToString: (ast: any) => string
  header: string
}

export function generateClient(options: GenerateClientOptions): Promise<void>
