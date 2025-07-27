import { visit } from 'unist-util-visit';

export function rehypeHandleRawNodes() {
  return (tree: any) => {
    visit(tree, 'raw', (node, index, parent) => {
      const commentRegex = /^<!--[\s\S]*-->$/;

      if (commentRegex.test(node.value)) {
        // If it's an HTML comment, remove it
        parent.children.splice(index, 1);
        return;
      } else {
        // If it's not an HTML comment, convert it to a text node without HTML-escaping
        node.type = 'text';
        // node.value remains as is
      }
    });
  };
}

