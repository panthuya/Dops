import re

with open('cmd/request.go', 'r') as f:
    content = f.read()

# 1. Remove "os/exec" from imports
content = content.replace('\t"os/exec"\n', '')

# 2. Remove grpcCmd definition (using regex carefully to match the block)
content = re.sub(r'var grpcCmd = &cobra\.Command\{.*?\n\}\n', '', content, flags=re.DOTALL)

# 3. Remove grpcCmd from init()
content = content.replace('\trootCmd.AddCommand(grpcCmd)\n', '')

# 4. Remove GRPC flags
content = content.replace('\t// GRPC flags\n\tgrpcCmd.Flags().StringSliceVarP(&headers, "header", "H", []string{}, "gRPC Header")\n', '')

with open('cmd/request.go', 'w') as f:
    f.write(content)
