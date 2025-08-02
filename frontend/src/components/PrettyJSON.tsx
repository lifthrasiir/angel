import type React from 'react';

interface PrettyJSONProps {
  data: any;
}

const PrettyJSON: React.FC<PrettyJSONProps> = ({ data }) => {
  const renderValue = (value: any) => {
    if (value === null || typeof value === 'boolean' || typeof value === 'number') {
      return String(value);
    } else if (typeof value === 'string') {
      if (value === '') {
        return <code>""</code>;
      }
      return <span style={{ whiteSpace: 'pre-wrap' }}>{value}</span>;
    } else if (Array.isArray(value)) {
      if (value.length === 0) {
        return <code>[]</code>;
      }
      return (
        <ul className="pretty-json-array">
          {value.map((item, index) => (
            <li key={index}>{renderValue(item)}</li>
          ))}
        </ul>
      );
    } else if (typeof value === 'object') {
      if (Object.keys(value).length === 0) {
        return <code>{'{}'}</code>;
      }
      return (
        <table className="pretty-json-object">
          <tbody>
            {Object.entries(value).map(([key, val]) => (
              <tr key={key}>
                <th style={{ whiteSpace: 'pre-wrap' }}>{key || <code>""</code>}</th>
                <td>{renderValue(val)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      );
    }
    return null;
  };

  return <div className="pretty-json-container">{renderValue(data)}</div>;
};

export default PrettyJSON;
