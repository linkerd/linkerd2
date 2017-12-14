import 'whatwg-fetch';

export const ApiHelpers = pathPrefix => {
  const podsPath = `${pathPrefix}/api/pods`;

  const apiFetch = path => {
    return fetch(path).then(handleFetchErr).then(r => r.json());
  };

  const fetchPods = () => {
    return apiFetch(podsPath);
  };

  const handleFetchErr = resp => {
    if (!resp.ok) {
      throw Error(resp.statusText);
    }
    return resp;
  };

  return {
    fetch: apiFetch,
    fetchPods
  };
};
